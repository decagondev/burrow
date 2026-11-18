// driver.go - the e1000 driver's probe and start: every PCI function of a
// model in Models (the 82540EM, and the 82574L since W28-S01), each reset,
// served on its interrupt (MSI on the 82574L, else its INTx line, maybe
// shared) and registered as the next ethN in PCI order (PIT-110). A card
// that fails to start is logged and skipped; the others still start.
package e1000

import (
	"errors"
	"fmt"

	"burrow/kernel/driver"
	"burrow/kernel/drivers/pci"
	"burrow/kernel/netdev"
)

func init() { driver.Register(&e1000Driver{}) }

type card struct {
	dev   pci.Device
	model Model
}

type e1000Driver struct{ cards []card }

func (*e1000Driver) Name() string { return "e1000" }

func (d *e1000Driver) Probe(env *driver.Env) bool {
	if env.Ports32 == nil || env.MapMMIO == nil || env.DMAPage == nil || env.IRQ == nil || !pci.Present(env.Ports32) {
		return false
	}
	for _, dev := range pci.Scan(env.Ports32) {
		if m, ok := ModelFor(dev.ID); ok && dev.Vendor == Vendor {
			d.cards = append(d.cards, card{dev, m})
		}
	}
	return len(d.cards) > 0
}

func (d *e1000Driver) Start(env *driver.Env) error {
	started := 0
	for _, c := range d.cards {
		if err := startCard(env, c); err != nil {
			env.Log.Warnf("%s at %v: %v", c.model.Name, c.dev, err)
			continue
		}
		started++
	}
	if started == 0 {
		return fmt.Errorf("none of %d cards started", len(d.cards))
	}
	return nil
}

func startCard(env *driver.Env, c card) error {
	base := c.dev.MemBase64(0)
	if base == 0 {
		return errors.New("BAR 0 is not memory")
	}
	// MSI where the model has it (its MSI-X wants an IVAR mapping a single
	// queue does not need); QEMU's 82540EM has neither: INTx.
	vec, err := pci.Setup(env, c.dev, true)
	if err != nil {
		return err
	}
	pci.EnableBusMaster(pci.PortConfig{IO: env.Ports32}, c.dev)
	mem, err := env.MapMMIO(base, MMIOBytes)
	if err != nil {
		return fmt.Errorf("map BAR 0 at %#x: %w", base, err)
	}
	nic, err := New(MMIO(mem), env.DMAPage, c.model)
	if err != nil {
		return err
	}
	handler := nic.Interrupt // an INTx line may be shared (W22-S01)
	if vec.Kind != pci.INTx {
		// A message is this card's alone, and its cause may already have
		// been taken by the previous call: never "nobody claimed".
		handler = func() bool { nic.Interrupt(); return true }
	}
	pci.Serve(env, vec, "e1000", handler)
	name := netdev.RegisterNext(nic)
	link := "down"
	if nic.LinkUp() {
		link = "up"
	}
	model := ""
	if c.model.Device != Models[0].Device {
		model = " " + c.model.Name + ","
	}
	env.Log.Infof("%s:%s %v, link %s, %v, %d-entry rings", name, model, nic.MAC(), link, vec, RingEntries)
	return nil
}
