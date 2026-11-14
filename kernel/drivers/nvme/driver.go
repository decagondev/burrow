// driver.go - the nvme driver's probe and start: every PCI function of
// class 01.08.02 (NVM Express), its registers mapped from BAR 0, bus
// mastering on, its interrupt chosen (MSI-X where it has it) before the
// reset, then namespace 1 set up and registered as nvme0n1, nvme1n1, ...
// in bus order.
package nvme

import (
	"fmt"
	"sync/atomic"
	"unsafe"

	"burrow/kernel/block"
	"burrow/kernel/driver"
	"burrow/kernel/drivers/pci"
)

const (
	classStorage = 0x01
	subclassNVM  = 0x08
	progIFNVMe   = 0x02
)

func init() { driver.Register(&nvmeDriver{}) }

type nvmeDriver struct{ devs []pci.Device }

func (*nvmeDriver) Name() string { return "nvme" }

func (n *nvmeDriver) Probe(env *driver.Env) bool {
	if env.Ports32 == nil || env.MapMMIO == nil || env.DMAPage == nil || env.IRQ == nil || !pci.Present(env.Ports32) {
		return false
	}
	for _, d := range pci.Scan(env.Ports32) {
		if d.Class == classStorage && d.Subclass == subclassNVM && d.ProgIF == progIFNVMe {
			n.devs = append(n.devs, d)
		}
	}
	return len(n.devs) > 0
}

func (n *nvmeDriver) Start(env *driver.Env) error {
	k := 0
	for _, dev := range n.devs {
		name := fmt.Sprintf("nvme%dn1", k)
		if err := start(env, dev, name); err != nil {
			env.Log.Warnf("%s (%v): %v", name, dev, err)
			continue
		}
		k++
	}
	if k == 0 {
		return fmt.Errorf("no usable NVMe controller of %d", len(n.devs))
	}
	return nil
}

type mmio []byte

func (m mmio) R32(off uint32) uint32 { return atomic.LoadUint32((*uint32)(unsafe.Pointer(&m[off]))) }
func (m mmio) W32(off uint32, v uint32) {
	atomic.StoreUint32((*uint32)(unsafe.Pointer(&m[off])), v)
}

func start(env *driver.Env, dev pci.Device, name string) error {
	base := dev.MemBase64(0)
	if base == 0 {
		return fmt.Errorf("BAR 0 is not memory")
	}
	vec, err := pci.Setup(env, dev, false)
	if err != nil {
		return err
	}
	pci.EnableBusMaster(pci.PortConfig{IO: env.Ports32}, dev)
	mem, err := env.MapMMIO(base, BAR0Bytes)
	if err != nil {
		return fmt.Errorf("map BAR 0 at %#x: %w", base, err)
	}
	d, err := New(mmio(mem), DMAPage(env.DMAPage))
	if err != nil {
		return err
	}
	pci.Serve(env, vec, "nvme", d.Interrupt)
	block.Register(name, d)
	env.Log.Infof("%s: %d MiB in %d-byte blocks, %q, %v", name, d.Sectors()*uint64(d.SectorSize())>>20,
		d.SectorSize(), d.Model, vec)
	return nil
}
