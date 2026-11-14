// driver.go - the ahci driver's probe and start: every PCI function of
// class 01.06.01 (SATA, AHCI programming interface), its registers mapped
// from BAR 5, bus mastering on, its interrupt (MSI where it has it) served,
// then each disk identified and registered as sda, sdb, ... in bus and port
// order.
package ahci

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
	subclassSATA = 0x06
	progIFAHCI   = 0x01
	abarBAR      = 5
)

func init() { driver.Register(&ahciDriver{}) }

type ahciDriver struct{ devs []pci.Device }

func (*ahciDriver) Name() string { return "ahci" }

func (a *ahciDriver) Probe(env *driver.Env) bool {
	if env.Ports32 == nil || env.MapMMIO == nil || env.DMAPage == nil || env.IRQ == nil || !pci.Present(env.Ports32) {
		return false
	}
	for _, d := range pci.Scan(env.Ports32) {
		if d.Class == classStorage && d.Subclass == subclassSATA && d.ProgIF == progIFAHCI {
			a.devs = append(a.devs, d)
		}
	}
	return len(a.devs) > 0
}

func (a *ahciDriver) Start(env *driver.Env) error {
	disks := 0
	for _, dev := range a.devs {
		n, err := start(env, dev, disks)
		if err != nil {
			env.Log.Warnf("%v: %v", dev, err)
		}
		disks += n
	}
	if disks == 0 {
		env.Log.Infof("no SATA disk on %d controller(s)", len(a.devs))
	}
	return nil
}

// mmio is the HBA's registers through the nucleus's uncached mapping.
type mmio []byte

func (m mmio) R32(off uint32) uint32 { return atomic.LoadUint32((*uint32)(unsafe.Pointer(&m[off]))) }
func (m mmio) W32(off uint32, v uint32) {
	atomic.StoreUint32((*uint32)(unsafe.Pointer(&m[off])), v)
}

// start drives one controller; its disks are named from sd<first>.
func start(env *driver.Env, dev pci.Device, first int) (int, error) {
	base := dev.MemBase(abarBAR)
	if base == 0 {
		return 0, fmt.Errorf("BAR %d is not memory", abarBAR)
	}
	vec, err := pci.Setup(env, dev, false)
	if err != nil {
		return 0, err
	}
	pci.EnableBusMaster(pci.PortConfig{IO: env.Ports32}, dev)
	mem, err := env.MapMMIO(base, ABARBytes)
	if err != nil {
		return 0, fmt.Errorf("map BAR %d at %#x: %w", abarBAR, base, err)
	}
	h, errs := New(mmio(mem), DMAPage(env.DMAPage))
	for _, e := range errs {
		env.Log.Warnf("%v", e)
	}
	pci.Serve(env, vec, "ahci", h.Interrupt)
	for _, e := range h.Identify() {
		env.Log.Warnf("%v", e)
	}
	for i, p := range h.Ports {
		name := fmt.Sprintf("sd%c", 'a'+first+i)
		block.Register(name, p)
		env.Log.Infof("%s: %d MiB, %q on port %d, %v", name, p.Sectors()*sectorBytes>>20, p.Model, p.Number, vec)
	}
	return len(h.Ports), nil
}
