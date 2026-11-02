// Package haltest provides in-memory test doubles for package hal.
package haltest

import "sync"

// FakePorts is a PortIO that records every write and serves reads from a
// map or a per-port hook. Liskov: anything that works with a real PortIO
// must work with this.
type FakePorts struct {
	mu     sync.Mutex
	Values map[uint16]uint8        // last value written / value to read
	Writes []Write                 // full write history, in order
	OnRead map[uint16]func() uint8 // optional dynamic read behaviour
}

// Write is one recorded Out8 call.
type Write struct {
	Port  uint16
	Value uint8
}

func NewFakePorts() *FakePorts {
	return &FakePorts{Values: map[uint16]uint8{}, OnRead: map[uint16]func() uint8{}}
}

func (f *FakePorts) In8(port uint16) uint8 {
	f.mu.Lock()
	defer f.mu.Unlock()
	if h, ok := f.OnRead[port]; ok {
		return h()
	}
	return f.Values[port]
}

func (f *FakePorts) Out8(port uint16, v uint8) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Values[port] = v
	f.Writes = append(f.Writes, Write{Port: port, Value: v})
}

// BytesWrittenTo returns every byte written to one port, in order.
func (f *FakePorts) BytesWrittenTo(port uint16) []byte {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []byte
	for _, w := range f.Writes {
		if w.Port == port {
			out = append(out, w.Value)
		}
	}
	return out
}
