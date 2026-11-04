package e1000

import (
	"errors"
	"fmt"

	"burrow/kernel/driver"
	"burrow/kernel/drivers/pci"
	"burrow/kernel/netdev"
)

func init() { driver.Register(&e1000Driver{}) }

type e1000Driver struct{ dev pci.Device }

func (*e1000Driver) Name() string { return "e1000" }

func (d *e1000Driver) Probe(env *driver.Env) bool {
	if env.Ports32 == nil || env.MapMMIO == nil || env.DMAPage == nil || env.IRQ == nil || !pci.Present(env.Ports32) {
		return false
	}
	var ok bool
	d.dev, ok = pci.Find(pci.Scan(env.Ports32), Vendor, Device)
	return ok
}

func (d *e1000Driver) Start(env *driver.Env) error {
	base := d.dev.MemBase(0)
	if base == 0 {
		return errors.New("BAR 0 is not memory")
	}
	if d.dev.IRQLine == 0 || d.dev.IRQLine >= 16 {
		return fmt.Errorf("no usable interrupt line (%d)", d.dev.IRQLine)
	}
	pci.EnableBusMaster(pci.PortConfig{IO: env.Ports32}, d.dev)
	mem, err := env.MapMMIO(base, MMIOBytes)
	if err != nil {
		return fmt.Errorf("map BAR 0 at %#x: %w", base, err)
	}
	nic, err := New(MMIO(mem), env.DMAPage)
	if err != nil {
		return err
	}
	driver.Serve(env.IRQ(int(d.dev.IRQLine)), nic.Interrupt)
	netdev.Register("eth0", nic)
	link := "down"
	if nic.LinkUp() {
		link = "up"
	}
	env.Log.Infof("eth0: %v, link %s, irq %d, %d-entry rings", nic.MAC(), link, d.dev.IRQLine, RingEntries)
	return nil
}
