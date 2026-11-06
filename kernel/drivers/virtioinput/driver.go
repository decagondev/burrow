// driver.go - the virtioinput driver's probe and start: find the device on
// PCI, its virtio capabilities, map each configuration structure, set the
// tablet up and serve its (shared) interrupt line. Uses only the modern
// interface (PIT-74): a device without the capabilities is refused.
package virtioinput

import (
	"fmt"

	"burrow/kernel/driver"
	"burrow/kernel/drivers/pci"
	"burrow/kernel/virtio"
)

func init() { driver.Register(&inputDriver{}) }

type inputDriver struct{ dev pci.Device }

func (*inputDriver) Name() string { return "virtioinput" }

func (d *inputDriver) Probe(env *driver.Env) bool {
	if env.Ports32 == nil || env.MapMMIO == nil || env.DMAPage == nil || env.IRQ == nil || !pci.Present(env.Ports32) {
		return false
	}
	var ok bool
	d.dev, ok = pci.Find(pci.Scan(env.Ports32), Vendor, Device)
	return ok
}

const pageBytes = 4096

// mapRegion maps one configuration structure, whole pages around it.
func (d *inputDriver) mapRegion(env *driver.Env, r virtio.Region) (virtio.MMIO, error) {
	if r.BAR > 5 {
		return nil, fmt.Errorf("capability names BAR %d", r.BAR)
	}
	base := d.dev.MemBase64(int(r.BAR))
	if base == 0 {
		return nil, fmt.Errorf("BAR %d is not memory", r.BAR)
	}
	pa := base + uint64(r.Offset)
	lo := pa &^ (pageBytes - 1)
	hi := (pa + uint64(r.Length) + pageBytes - 1) &^ (pageBytes - 1)
	mem, err := env.MapMMIO(lo, hi-lo)
	if err != nil {
		return nil, fmt.Errorf("map BAR %d at %#x: %w", r.BAR, lo, err)
	}
	return virtio.MMIO(mem[pa-lo : pa-lo+uint64(r.Length)]), nil
}

func (d *inputDriver) Start(env *driver.Env) error {
	if d.dev.IRQLine == 0 || d.dev.IRQLine >= 16 {
		return fmt.Errorf("no usable interrupt line (%d)", d.dev.IRQLine)
	}
	cfg := pci.PortConfig{IO: env.Ports32}
	pci.EnableBusMaster(cfg, d.dev)
	caps, err := virtio.FindCaps(func(off uint8) uint32 { return cfg.Read32(d.dev.Bus, d.dev.Dev, d.dev.Fn, off) })
	if err != nil {
		return err
	}
	vd := &virtio.Device{NotifyMultiplier: caps.NotifyMultiplier}
	for _, m := range []struct {
		r   virtio.Region
		dst *virtio.Regs
	}{{caps.Common, &vd.Common}, {caps.Notify, &vd.Notify}, {caps.ISR, &vd.ISR}, {caps.Device, &vd.Config}} {
		mem, err := d.mapRegion(env, m.r)
		if err != nil {
			return err
		}
		*m.dst = mem
	}
	first := true
	var sink func(typ, code uint16, value int32) // set before the line is served
	emit := func(e Event) {
		if first {
			first = false
			env.Log.Infof("first event: type %d code %d value %d", e.Type, e.Code, e.Value)
		}
		if sink != nil {
			sink(e.Type, e.Code, e.Value)
		}
	}
	tab, err := New(vd, DMAPage(env.DMAPage), emit)
	if err != nil {
		return err
	}
	if env.Tablet != nil && tab.HasAbs {
		sink = env.Tablet(driver.TabletInfo{Name: tab.Name, XMin: tab.X.Min, XMax: tab.X.Max, YMin: tab.Y.Min, YMax: tab.Y.Max})
	}
	if env.Lines != nil {
		env.Lines.Serve(int(d.dev.IRQLine), "virtioinput", tab.Interrupt)
	} else {
		driver.Serve(env.IRQ(int(d.dev.IRQLine)), func() { tab.Interrupt() })
	}
	if tab.HasAbs {
		env.Log.Infof("%s, abs x %v y %v, irq %d", tab.Name, tab.X, tab.Y, d.dev.IRQLine)
	} else {
		env.Log.Infof("%s, no absolute axes, irq %d", tab.Name, d.dev.IRQLine)
	}
	return nil
}
