package evdev

// mice.go - /dev/input/mice (W14-S02): the mouse as the PS/2 byte stream
// older programs read, Linux's mousedev. Each open file gets 3-byte packets
// (buttons and sign bits, dx, dy with up positive) until the program writes
// the IntelliMouse handshake (set sample rate 200, 100, 80); then 4-byte
// packets carry the wheel. Commands written to it are answered the way a
// PS/2 mouse would (ACK 0xFA, the device id for 0xF2, 0xAA 0x00 after a
// reset). Never blocks the driver: a full queue drops packets.

import (
	"sync"
	"syscall"

	"burrow/kernel/fs/devfs"
)

const (
	miceQueue = 4096 // bytes per open file
	psAck     = 0xFA
	psSync    = 0x08 // bit 3 of byte 0 is always set
	psXSign   = 0x10
	psYSign   = 0x20
	idPS2     = 0x00
	idIMPS2   = 0x03
	maxDelta  = 127
)

// Mice is /dev/input/mice.
type Mice struct {
	mu    sync.Mutex
	opens map[*miceHandle]bool
}

func NewMice() *Mice { return &Mice{opens: map[*miceHandle]bool{}} }

func (m *Mice) Open() (devfs.Device, error) {
	h := &miceHandle{m: m, changed: make(chan struct{})}
	m.mu.Lock()
	m.opens[h] = true
	m.mu.Unlock()
	return h, nil
}

func (m *Mice) Read([]byte) (int, error)  { return 0, syscall.EAGAIN }
func (m *Mice) Write([]byte) (int, error) { return 0, syscall.EINVAL }

// feed queues one packet (screen directions, as Mouse.Feed) for every open
// file, split into several when a move is larger than a packet holds.
func (m *Mice) feed(dx, dy, wheel int, buttons uint8) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for first := true; first || dx != 0 || dy != 0; first = false {
		px, py := clamp(dx), clamp(-dy) // PS/2: y grows upward
		dx, dy = dx-px, dy+py
		b0 := psSync | buttons&7
		if px < 0 {
			b0 |= psXSign
		}
		if py < 0 {
			b0 |= psYSign
		}
		for h := range m.opens {
			h.packet(b0, byte(px), byte(py), byte(-clampWheel(wheel)))
		}
		wheel = 0
	}
}

func clamp(v int) int { return max(-maxDelta, min(maxDelta, v)) }

func clampWheel(v int) int { return max(-7, min(7, v)) }

type miceHandle struct {
	m       *Mice
	mu      sync.Mutex
	queue   []byte
	changed chan struct{}
	imps    bool
	rates   [3]byte // the last three sample rates set
	rateArg bool    // the next byte written is a sample rate
}

// packet queues one packet (m.mu held).
func (h *miceHandle) packet(b0, x, y, z byte) {
	p := []byte{b0, x, y}
	if h.imps {
		p = append(p, z)
	}
	h.queueBytes(p...)
}

func (h *miceHandle) queueBytes(b ...byte) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.queue)+len(b) > miceQueue {
		return
	}
	h.queue = append(h.queue, b...)
	close(h.changed)
	h.changed = make(chan struct{})
}

func (h *miceHandle) Read(b []byte) (int, error) { return h.ReadCancel(b, nil) }

// ReadCancel returns what is queued, waiting for at least one byte.
func (h *miceHandle) ReadCancel(b []byte, cancel <-chan struct{}) (int, error) {
	for {
		h.mu.Lock()
		if len(h.queue) > 0 {
			n := copy(b, h.queue)
			h.queue = h.queue[n:]
			h.mu.Unlock()
			return n, nil
		}
		ch := h.changed
		h.mu.Unlock()
		select {
		case <-ch:
		case <-cancel:
			return 0, syscall.EINTR
		}
	}
}

// Write takes PS/2 mouse commands and queues the answers.
func (h *miceHandle) Write(b []byte) (int, error) {
	for _, c := range b {
		h.mu.Lock()
		rate := h.rateArg
		h.rateArg = false
		if rate {
			h.rates = [3]byte{h.rates[1], h.rates[2], c}
			if h.rates == [3]byte{200, 100, 80} {
				h.imps = true
			}
		}
		h.mu.Unlock()
		switch {
		case rate:
			h.queueBytes(psAck)
		case c == 0xF3: // set sample rate: the argument follows
			h.mu.Lock()
			h.rateArg = true
			h.mu.Unlock()
			h.queueBytes(psAck)
		case c == 0xF2: // get device id
			id := byte(idPS2)
			h.mu.Lock()
			if h.imps {
				id = idIMPS2
			}
			h.mu.Unlock()
			h.queueBytes(psAck, id)
		case c == 0xFF: // reset
			h.mu.Lock()
			h.imps = false
			h.mu.Unlock()
			h.queueBytes(psAck, 0xAA, 0x00)
		case c == 0xE9: // status request: stream mode, resolution 2, 100 samples/s
			h.queueBytes(psAck, 0x00, 0x02, 100)
		default:
			h.queueBytes(psAck)
		}
	}
	return len(b), nil
}

func (h *miceHandle) WriteCancel(b []byte, _ <-chan struct{}) (int, error) { return h.Write(b) }

func (h *miceHandle) Poll() (int16, <-chan struct{}) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.queue) > 0 {
		return 0x1, h.changed
	}
	return 0x4, h.changed // POLLOUT: commands are always accepted
}

func (h *miceHandle) Close() error {
	h.m.mu.Lock()
	delete(h.m.opens, h)
	h.m.mu.Unlock()
	return nil
}
