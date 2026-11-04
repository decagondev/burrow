// Package netdev is where network cards meet what uses them (W17-S01): a
// driver registers its card as a Device under a name (eth0, ...), and the
// network stack (W17-S02) or a boot check finds it there. Frames are whole
// Ethernet frames without the CRC, which the card adds and strips. Never
// imports a driver: drivers import it. Docs:
// docs/04-plan/waves/WAVE-17-networking.md.
package netdev

import (
	"errors"
	"sort"
	"sync"
)

// MAC is an Ethernet address.
type MAC [6]byte

// Device is one network card.
type Device interface {
	MAC() MAC
	MTU() int // the largest payload a frame may carry (1500 for Ethernet)
	LinkUp() bool
	// Send queues one frame; ErrBusy means the card's queue is full for now.
	// It never blocks and may be called from any goroutine.
	Send(frame []byte) error
	// SetReceiver installs the function every received frame goes to (nil
	// drops them). It runs on the driver's interrupt goroutine; the frame is
	// the receiver's to keep.
	SetReceiver(func(frame []byte))
	Stats() Stats
}

// Stats counts what a card did since it started.
type Stats struct {
	RxFrames, RxBytes, RxDropped uint64
	TxFrames, TxBytes, TxBusy    uint64
}

// ErrBusy is Send's answer when the transmit queue has no room.
var ErrBusy = errors.New("netdev: transmit queue full")

var (
	mu      sync.Mutex
	devices = map[string]Device{}
)

// Register makes d known as name; a second card with the same name is a
// driver bug.
func Register(name string, d Device) {
	mu.Lock()
	defer mu.Unlock()
	if _, dup := devices[name]; dup {
		panic("netdev: " + name + " registered twice")
	}
	devices[name] = d
}

// Get returns the card registered as name.
func Get(name string) (Device, bool) {
	mu.Lock()
	defer mu.Unlock()
	d, ok := devices[name]
	return d, ok
}

// Names lists the registered cards in order.
func Names() []string {
	mu.Lock()
	defer mu.Unlock()
	names := make([]string, 0, len(devices))
	for n := range devices {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}
