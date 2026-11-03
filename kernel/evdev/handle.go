package evdev

// handle.go - one open event device (keyboard or mouse): its event queue,
// blocking and cancellable reads of whole events, and readiness for poll.
// The ioctls are in ioctl.go. Never holds the device's lock and its own at
// once except in push (device, then handle).

import (
	"sync"
	"syscall"
)

type handle struct {
	d       *core
	mu      sync.Mutex
	queue   []byte // whole events
	changed chan struct{}
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
