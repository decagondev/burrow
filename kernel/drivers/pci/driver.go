package pci

import (
	"errors"
	"sync"

	"burrow/kernel/driver"
)

func init() { driver.Register(&pciDriver{}) }

type pciDriver struct{}

var (
	mu    sync.Mutex
	found []Device
)

// Devices is what the boot scan found (for `lspci`).
func Devices() []Device {
	mu.Lock()
	defer mu.Unlock()
	return append([]Device(nil), found...)
}

func (*pciDriver) Name() string { return "pci" }

func (*pciDriver) Probe(env *driver.Env) bool {
	return env.Ports32 != nil && Present(env.Ports32)
}

func (*pciDriver) Start(env *driver.Env) error {
	devs := Enumerate(PortConfig{IO: env.Ports32})
	if len(devs) == 0 {
		return errors.New("no functions on any bus")
	}
	for _, d := range devs {
		env.Log.Infof("%v", d)
	}
	mu.Lock()
	found = devs
	mu.Unlock()
	return nil
}
