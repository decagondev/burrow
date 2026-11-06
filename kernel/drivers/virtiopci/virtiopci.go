// Package virtiopci opens a virtio 1.0 PCI device for a driver (W23-S04,
// from virtioinput's W22 code): bus mastering on, the four vendor
// capabilities found, each configuration structure mapped on whole pages.
// Only the modern interface (PIT-74): a device without the capabilities is
// refused. Never touches the device's queues or features: kernel/virtio does.
// Docs: docs/04-plan/waves/WAVE-23-proc-virtio.md.
package virtiopci

import (
	"fmt"

	"burrow/kernel/driver"
	"burrow/kernel/drivers/pci"
	"burrow/kernel/virtio"
)

const pageBytes = 4096

// Usable says whether env has what a virtio driver needs.
func Usable(env *driver.Env) bool {
	return env.Ports32 != nil && env.MapMMIO != nil && env.DMAPage != nil && env.IRQ != nil && pci.Present(env.Ports32)
}

// FindAll is every device on the bus with vendor and one of ids, in bus order.
func FindAll(env *driver.Env, vendor uint16, ids ...uint16) []pci.Device {
	var out []pci.Device
	for _, d := range pci.Scan(env.Ports32) {
		for _, id := range ids {
			if d.Vendor == vendor && d.ID == id {
				out = append(out, d)
			}
		}
	}
	return out
}

// Open makes dev a virtio.Device: bus master, capabilities, mapped regions.
func Open(env *driver.Env, dev pci.Device) (*virtio.Device, error) {
	if dev.IRQLine == 0 || dev.IRQLine >= 16 {
		return nil, fmt.Errorf("no usable interrupt line (%d)", dev.IRQLine)
	}
	cfg := pci.PortConfig{IO: env.Ports32}
	pci.EnableBusMaster(cfg, dev)
	caps, err := virtio.FindCaps(func(off uint8) uint32 { return cfg.Read32(dev.Bus, dev.Dev, dev.Fn, off) })
	if err != nil {
		return nil, err
	}
	vd := &virtio.Device{NotifyMultiplier: caps.NotifyMultiplier}
	for _, m := range []struct {
		r   virtio.Region
		dst *virtio.Regs
	}{{caps.Common, &vd.Common}, {caps.Notify, &vd.Notify}, {caps.ISR, &vd.ISR}, {caps.Device, &vd.Config}} {
		mem, err := mapRegion(env, dev, m.r)
		if err != nil {
			return nil, err
		}
		*m.dst = mem
	}
	return vd, nil
}

// mapRegion maps one configuration structure, whole pages around it.
func mapRegion(env *driver.Env, dev pci.Device, r virtio.Region) (virtio.MMIO, error) {
	if r.BAR > 5 {
		return nil, fmt.Errorf("capability names BAR %d", r.BAR)
	}
	base := dev.MemBase64(int(r.BAR))
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

// Serve runs handler on dev's interrupt line, shared when env has lines.
func Serve(env *driver.Env, dev pci.Device, name string, handler func() bool) {
	if env.Lines != nil {
		env.Lines.Serve(int(dev.IRQLine), name, handler)
		return
	}
	driver.Serve(env.IRQ(int(dev.IRQLine)), func() { handler() })
}
