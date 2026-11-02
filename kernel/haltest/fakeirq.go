package haltest

import (
	"errors"
	"sync"
	"sync/atomic"
)

// ErrClosed is what FakeIRQ.Wait returns once the line is closed, so a
// driver goroutine under test can be stopped.
var ErrClosed = errors.New("haltest: irq line closed")

// FakeIRQ is a hal.IRQLine driven by the test: Fire plays the device raising
// the line. Like the nucleus, an interrupt with no waiter stays pending and
// several fires before a Wait count once.
type FakeIRQ struct {
	fired     chan struct{}
	closed    chan struct{}
	closeOnce sync.Once
	acks      atomic.Int64
}

func NewFakeIRQ() *FakeIRQ {
	return &FakeIRQ{fired: make(chan struct{}, 1), closed: make(chan struct{})}
}

// Fire never blocks.
func (q *FakeIRQ) Fire() {
	select {
	case q.fired <- struct{}{}:
	default: // already pending
	}
}

func (q *FakeIRQ) Wait() error {
	select {
	case <-q.fired:
		return nil
	case <-q.closed:
		return ErrClosed
	}
}

func (q *FakeIRQ) Ack()      { q.acks.Add(1) }
func (q *FakeIRQ) Acks() int { return int(q.acks.Load()) }
func (q *FakeIRQ) Close()    { q.closeOnce.Do(func() { close(q.closed) }) }
