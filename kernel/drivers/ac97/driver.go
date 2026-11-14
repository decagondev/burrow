// driver.go - the ac97 driver's probe and start: find the card on PCI, set
// it up, serve its (maybe shared) interrupt line, and make /dev/dsp.
package ac97

import (
	"burrow/kernel/driver"
	"burrow/kernel/drivers/pci"
	"burrow/kernel/sound"
)

func init() { driver.Register(&ac97Driver{}) }

type ac97Driver struct{ dev pci.Device }

func (*ac97Driver) Name() string { return "ac97" }

func (d *ac97Driver) Probe(env *driver.Env) bool {
	if env.Ports == nil || env.Ports16 == nil || env.Ports32 == nil || env.DMAPage == nil || env.IRQ == nil ||
		env.Dev == nil || !pci.Present(env.Ports32) {
		return false
	}
	var ok bool
	d.dev, ok = pci.Find(pci.Scan(env.Ports32), Vendor, Device)
	return ok && d.dev.IOBase(0) != 0 && d.dev.IOBase(1) != 0
}

func (d *ac97Driver) Start(env *driver.Env) error {
	vec, err := pci.Setup(env, d.dev, false)
	if err != nil {
		return err
	}
	pci.EnableIOBusMaster(pci.PortConfig{IO: env.Ports32}, d.dev)
	p := Ports{IO8: env.Ports, IO16: env.Ports16, IO32: env.Ports32, NAM: d.dev.IOBase(0), NABM: d.dev.IOBase(1)}
	card, err := New(p, DMAPage(env.DMAPage))
	if err != nil {
		return err
	}
	pci.Serve(env, vec, "ac97", card.Interrupt)
	if err := env.Dev.Register("dsp", sound.NewDSP(card), 0o666); err != nil {
		return err
	}
	vra := "fixed"
	if card.VRA() {
		vra = "vra"
	}
	env.Log.Infof("ac97 at io %#x/%#x, codec ready, %d Hz (%s), %v", p.NAM, p.NABM, card.rate, vra, vec)
	return nil
}
