package evdev

// handle.go - one open /dev/input/event0: its event queue, blocking and
// cancellable reads of whole events, readiness for poll, and the ioctls
// evtest-style programs ask first. Never holds the device's lock and its
// own at once except in push (device, then handle).

import (
	"encoding/binary"
	"sync"
	"syscall"
)

// ioctls (<linux/input.h>): _IOR('E', nr, size) = 0x80000000 | size<<16 | 'E'<<8 | nr.
const (
	eviocgversion = 0x80044501
	eviocgid      = 0x80084502
	eviocgnameNr  = 0x06
	eviocgrab     = 0x40044590
	iocMaskNoSize = 0xC000FFFF // direction, type and number without the size
	iocRead       = 0x80000000

	version = 0x010001
	name    = "Burrow PS/2 keyboard"
)

type handle struct {
	d       *Device
	mu      sync.Mutex
	queue   []byte // whole events
	changed chan struct{}
	closed  bool
}

// push queues ev unless the queue is full (d.mu held).
func (h *handle) push(ev []byte) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.queue)+len(ev) > queueEvents*EventSize {
		return
	}
	h.queue = append(h.queue, ev...)
	close(h.changed)
	h.changed = make(chan struct{})
}

func (h *handle) Read(b []byte) (int, error) { return h.ReadCancel(b, nil) }

// ReadCancel returns as many whole events as fit, waiting for at least one
// (EINVAL if b cannot hold one, EINTR on cancel).
func (h *handle) ReadCancel(b []byte, cancel <-chan struct{}) (int, error) {
	if len(b) < EventSize {
		return 0, syscall.EINVAL
	}
	for {
		h.mu.Lock()
		if len(h.queue) > 0 {
			n := min(len(b)/EventSize*EventSize, len(h.queue))
			copy(b, h.queue[:n])
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

func (h *handle) Write([]byte) (int, error) { return 0, syscall.EINVAL }

func (h *handle) WriteCancel(b []byte, _ <-chan struct{}) (int, error) { return h.Write(b) }

// Poll: readable with events queued (proc.Poller).
func (h *handle) Poll() (int16, <-chan struct{}) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.queue) > 0 {
		return 0x1, h.changed // POLLIN
	}
	return 0, h.changed
}

func (h *handle) Close() error {
	h.d.mu.Lock()
	delete(h.d.opens, h)
	h.d.mu.Unlock()
	return nil
}

// Ioctl: the device's version, id and name, and EVIOCGRAB (always granted:
// an open device already has the keyboard).
func (h *handle) Ioctl(req, arg uint64, in, out func(va uint64, b []byte) error) (int64, error) {
	switch {
	case req == eviocgversion:
		return 0, out(arg, binary.LittleEndian.AppendUint32(nil, version))
	case req == eviocgid:
		return 0, out(arg, []byte{0x11, 0, 1, 0, 1, 0, 1, 0}) // BUS_I8042, vendor 1, product 1, version 1
	case req&iocMaskNoSize == iocRead|'E'<<8|eviocgnameNr:
		size := int(req >> 16 & 0x3FFF)
		b := append([]byte(name), 0)
		if len(b) > size {
			b = b[:size]
		}
		return int64(len(b)), out(arg, b)
	case req == eviocgrab:
		return 0, nil
	}
	return 0, syscall.ENOTTY
}
