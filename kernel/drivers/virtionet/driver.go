// driver.go - the virtionet driver's probe and start: the first virtio
// network card on PCI (the modern 1af4:1041, or the transitional 1af4:1000
// through its modern capabilities only), set up, served on its interrupt
// line and registered as eth0, or eth1 when an e1000 has eth0.
package virtionet

import (
	"fmt"

	"burrow/kernel/driver"
	"burrow/kernel/drivers/pci"
	"burrow/kernel/drivers/virtiopci"
	"burrow/kernel/netdev"
)

// PCI identity: virtio's vendor, the modern network card (0x1040 + 1) and the transitional one.
const (
	Vendor       = 0x1AF4
	Modern       = 0x1041
	Transitional = 0x1000
)

func init() { driver.Register(&netDriver{}) }

type netDriver struct{ dev pci.Device }

func (*netDriver) Name() string { return "virtionet" }

func (d *netDriver) Probe(env *driver.Env) bool {
	if !virtiopci.Usable(env) {
		return false
	}
	devs := virtiopci.FindAll(env, Vendor, Modern, Transitional)
	if len(devs) == 0 {
		return false
	}
	d.dev = devs[0]
	return true
}

func (d *netDriver) Start(env *driver.Env) error {
	vd, err := virtiopci.Open(env, d.dev)
	if err != nil {
		return err
	}
	nic, err := New(vd, DMAPage(env.DMAPage))
	if err != nil {
		return err
	}
	virtiopci.Serve(env, d.dev, "virtionet", nic.Interrupt)
	name := "eth0"
	if _, taken := netdev.Get(name); taken {
		name = "eth1"
	}
	netdev.Register(name, nic)
	link := "down"
	if nic.LinkUp() {
		link = "up"
	}
	env.Log.Infof("%s %s, link %s, irq %d", name, fmt.Sprint(macString(nic.MAC())), link, d.dev.IRQLine)
	return nil
}

func macString(m netdev.MAC) string {
	return fmt.Sprintf("%02x:%02x:%02x:%02x:%02x:%02x", m[0], m[1], m[2], m[3], m[4], m[5])
}
