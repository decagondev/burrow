// driver.go - the virtioinput driver's probe and start: find the device on
// PCI, open it (drivers/virtiopci: capabilities, mapped configuration),
// set the tablet up and serve its (shared) interrupt line. Uses only the
// modern interface (PIT-74): a device without the capabilities is refused.
package virtioinput

import (
	"burrow/kernel/driver"
	"burrow/kernel/drivers/pci"
	"burrow/kernel/drivers/virtiopci"
)

func init() { driver.Register(&inputDriver{}) }

type inputDriver struct{ dev pci.Device }

func (*inputDriver) Name() string { return "virtioinput" }

func (d *inputDriver) Probe(env *driver.Env) bool {
	if !virtiopci.Usable(env) {
		return false
	}
	var ok bool
	d.dev, ok = pci.Find(pci.Scan(env.Ports32), Vendor, Device)
	return ok
}

func (d *inputDriver) Start(env *driver.Env) error {
	vd, err := virtiopci.Open(env, d.dev)
	if err != nil {
		return err
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
	virtiopci.Serve(env, d.dev, "virtioinput", tab.Interrupt)
	if tab.HasAbs {
		env.Log.Infof("%s, abs x %v y %v, irq %d", tab.Name, tab.X, tab.Y, d.dev.IRQLine)
	} else {
		env.Log.Infof("%s, no absolute axes, irq %d", tab.Name, d.dev.IRQLine)
	}
	return nil
}
