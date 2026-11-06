// driver.go - the virtioblk driver's probe and start: every virtio block
// device on PCI (the modern 1af4:1042, and the transitional 1af4:1001
// through its modern capabilities only), opened, set up, served on its
// interrupt line and registered as vda, vdb, ... in bus order.
package virtioblk

import (
	"fmt"

	"burrow/kernel/block"
	"burrow/kernel/driver"
	"burrow/kernel/drivers/pci"
	"burrow/kernel/drivers/virtiopci"
)

// PCI identity: virtio's vendor, the modern block device (0x1040 + 2) and the transitional one.
const (
	Vendor       = 0x1AF4
	Modern       = 0x1042
	Transitional = 0x1001
)

func init() { driver.Register(&blkDriver{}) }

type blkDriver struct{ devs []pci.Device }

func (*blkDriver) Name() string { return "virtioblk" }

func (b *blkDriver) Probe(env *driver.Env) bool {
	if !virtiopci.Usable(env) {
		return false
	}
	b.devs = virtiopci.FindAll(env, Vendor, Modern, Transitional)
	return len(b.devs) > 0
}

func (b *blkDriver) Start(env *driver.Env) error {
	n := 0
	for _, dev := range b.devs {
		name := fmt.Sprintf("vd%c", 'a'+n)
		if err := start(env, dev, name); err != nil {
			env.Log.Warnf("%s (%s): %v", name, dev, err)
			continue
		}
		n++
	}
	if n == 0 {
		return fmt.Errorf("no usable virtio block device of %d", len(b.devs))
	}
	return nil
}

func start(env *driver.Env, dev pci.Device, name string) error {
	vd, err := virtiopci.Open(env, dev)
	if err != nil {
		return err
	}
	d, err := New(vd, DMAPage(env.DMAPage))
	if err != nil {
		return err
	}
	virtiopci.Serve(env, dev, "virtioblk", d.Interrupt)
	block.Register(name, d.Device())
	mode := "read-write"
	if d.ReadOnly {
		mode = "read-only"
	}
	env.Log.Infof("%s %d MiB, %s, flush %v, irq %d", name, d.Sectors()*sectorBytes>>20, mode, d.CanFlush, dev.IRQLine)
	return nil
}
