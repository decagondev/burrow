// tablet.go - the event queue: one DMA page of rings, one of 8-byte event
// buffers, every buffer offered to the device; an interrupt takes back what
// the device filled, hands each event on and offers the buffer again. Never
// waits; the handler runs only on its line's goroutine.
package virtioinput

import (
	"errors"
	"fmt"

	"burrow/kernel/virtio"
)

// DMAPage hands out one zeroed DMA page (driver.Env.DMAPage).
type DMAPage func() (pa uint64, mem []byte, err error)

// Tablet is a set-up virtio input device.
type Tablet struct {
	dev     *virtio.Device
	q       *virtio.Queue
	bufs    []byte
	emit    func(Event)
	Name    string
	X, Y    Axis
	HasAbs  bool
	Events  uint64 // events handed on, for the log and tests
	Unknown uint64 // used entries naming no buffer of ours
}

// New sets the device up: features, its name and axes, queue 0 filled.
// emit gets every event; it must not block.
func New(dev *virtio.Device, dma DMAPage, emit func(Event)) (*Tablet, error) {
	if _, err := dev.Negotiate(0); err != nil {
		return nil, err
	}
	t := &Tablet{dev: dev, emit: emit, Name: Name(dev.Config)}
	x, okx := AbsRange(dev.Config, absX)
	y, oky := AbsRange(dev.Config, absY)
	t.X, t.Y, t.HasAbs = x, y, okx && oky
	size := dev.QueueMax(0)
	if size == 0 {
		return nil, errors.New("virtioinput: no event queue")
	}
	if size > maxQueue {
		size = maxQueue
	}
	qpa, qmem, err := dma()
	if err != nil {
		return nil, err
	}
	bpa, bmem, err := dma()
	if err != nil {
		return nil, err
	}
	if int(size)*eventBytes > len(bmem) {
		return nil, fmt.Errorf("virtioinput: %d events do not fit a page", size)
	}
	if t.q, err = virtio.NewQueue(0, size, qpa, qmem); err != nil {
		return nil, err
	}
	t.bufs = bmem[:int(size)*eventBytes]
	if err := dev.SetupQueue(t.q); err != nil {
		return nil, err
	}
	for i := uint16(0); i < size; i++ {
		t.q.SetDesc(i, bpa+uint64(i)*eventBytes, eventBytes, virtio.DescWrite)
		t.q.Offer(i)
	}
	dev.Ready()
	dev.Kick(t.q)
	return t, nil
}

// Interrupt handles the device's interrupt; false means it did not raise it.
func (t *Tablet) Interrupt() bool {
	isr := t.dev.ReadISR()
	if isr == 0 {
		return false
	}
	if isr&virtio.ISRQueue != 0 {
		t.drain()
	}
	return true
}

func (t *Tablet) drain() {
	took := false
	for {
		id, n, ok := t.q.Used()
		if !ok {
			break
		}
		if id >= t.q.Size {
			t.Unknown++
			continue
		}
		if n >= eventBytes {
			e := decode(t.bufs[int(id)*eventBytes:])
			t.Events++
			if t.emit != nil {
				t.emit(e)
			}
		}
		t.q.Offer(id)
		took = true
	}
	if took {
		t.dev.Kick(t.q)
	}
}
