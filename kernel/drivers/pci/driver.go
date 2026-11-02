package pci

import (
	"errors"
	"sync"

	"burrow/kernel/driver"
	"burrow/kernel/hal"
)

func init() { driver.Register(&pciDriver{}) }

type pciDriver struct{}

var (
	mu      sync.Mutex
	scanned bool
	found   []Device
)

// Scan enumerates the bus once; later calls return the same list. Drivers
// that look for their device call it, whatever order they start in.
func Scan(io hal.PortIO32) []Device {
	mu.Lock()
	defer mu.Unlock()
	if !scanned {
		found, scanned = Enumerate(PortConfig{IO: io}), true
	}
	return append([]Device(nil), found...)
}

// Devices is what the scan found (nil before any Scan): for `lspci`.
func Devices() []Device {
	mu.Lock()
	defer mu.Unlock()
	return append([]Device(nil), found...)
}

// Find returns the first function with that vendor and device ID.
func Find(devs []Device, vendor, id uint16) (Device, bool) {
	for _, d := range devs {
		if d.Vendor == vendor && d.ID == id {
			return d, true
		}
	}
	return Device{}, false
}

func (*pciDriver) Name() string { return "pci" }

func (*pciDriver) Probe(env *driver.Env) bool {
	return env.Ports32 != nil && Present(env.Ports32)
}

func (*pciDriver) Start(env *driver.Env) error {
	devs := Scan(env.Ports32)
	if len(devs) == 0 {
		return errors.New("no functions on any bus")
	}
	for _, d := range devs {
		env.Log.Infof("%v", d)
	}
	return nil
}
