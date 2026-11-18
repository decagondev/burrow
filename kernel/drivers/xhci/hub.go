// hub.go is the controller's side of a USB hub (W28-S02; spec §4.3, §4.5.2,
// §6.2.2): a Device is a usb.HubHost, so package usb's hub driver can mark
// it a hub (its slot context's Hub, ports and TT think time, by Configure
// Endpoint) and ask for the devices on its ports to be addressed with a
// route string, its root-hub port and, for a low- or full-speed device
// behind a high-speed hub, that hub's slot and port as its transaction
// translator (PIT-107). Detaching a device detaches what is below it,
// ends its pipes and gives its slot back. Never resets a hub's port: the
// hub driver does that with the hub's own requests.
package xhci

import (
	"fmt"

	"burrow/kernel/usb"
)

// slotInfo is the slot context d's place, speed and hub state make.
func (d *Device) slotInfo() slotInfo {
	d.cmu.Lock()
	defer d.cmu.Unlock()
	return slotInfo{rootPort: d.place.rootPort, route: d.place.route, speed: d.Speed, lastDCI: d.lastDCI,
		ttSlot: d.place.ttSlot, ttPort: d.place.ttPort,
		hub: d.hub, multiTT: d.multiTT, ports: d.hubPorts, thinkTime: d.think}
}

// attached is d as usb.Connect takes it.
func (d *Device) attached() usb.Attached {
	return usb.Attached{Dev: d, Path: d.place.path, Speed: d.Speed, Desc: d.Desc, Config: d.Config, Product: d.Product}
}

func (d *Device) Path() string         { return d.place.path }
func (d *Device) Depth() int           { return d.place.depth }
func (d *Device) LinkSpeed() usb.Speed { return d.Speed }

// MakeHub records the hub's ports and TT in its slot context.
func (d *Device) MakeHub(ports, thinkTime int, multiTT bool) error {
	d.cmu.Lock()
	d.hub, d.hubPorts, d.think, d.multiTT = true, ports, thinkTime, multiTT
	d.cmu.Unlock()
	d.mu.Lock()
	defer d.mu.Unlock()
	l := d.h.ctx
	l.inputControl(d.in, 1<<0) // the slot context alone
	l.slotContext(d.in, d.slotInfo())
	_, err := d.h.command(TRB{Param: d.inPA, Control: cmdConfigureEP<<ctlTypeSh | uint32(d.Slot)<<ctlSlotSh})
	return err
}

// childPlace is where the device on the hub's port is.
func (d *Device) childPlace(port int, speed usb.Speed) place {
	p := place{rootPort: d.place.rootPort, route: childRoute(d.place.route, d.place.depth, port),
		depth: d.place.depth + 1, path: fmt.Sprintf("%s.%d", d.place.path, port)}
	switch {
	case speed == usb.SpeedHigh || speed == usb.SpeedSuper:
		// no translator
	case d.Speed == usb.SpeedHigh:
		p.ttSlot, p.ttPort = d.Slot, port // this hub translates
	default:
		p.ttSlot, p.ttPort = d.place.ttSlot, d.place.ttPort // the one above it does, if any
	}
	return p
}

// Attach addresses the device the hub enabled on port.
func (d *Device) Attach(port int, speed usb.Speed) (usb.Attached, error) {
	d.cmu.Lock()
	isHub := d.hub
	d.cmu.Unlock()
	if !isHub {
		return usb.Attached{}, fmt.Errorf("slot %d is not a hub", d.Slot)
	}
	c, err := d.h.enumerate(d.childPlace(port, speed), speed)
	if err != nil {
		return usb.Attached{}, err
	}
	d.cmu.Lock()
	old := d.children[port]
	d.children[port] = c
	d.cmu.Unlock()
	if old != nil {
		old.release()
	}
	return c.attached(), nil
}

// Detach forgets the device on port and everything below it.
func (d *Device) Detach(port int) {
	d.cmu.Lock()
	c := d.children[port]
	delete(d.children, port)
	d.cmu.Unlock()
	if c != nil {
		c.release()
	}
}

// release ends d: its children first, then its pipes (gone), then its slot.
func (d *Device) release() {
	d.cmu.Lock()
	kids := d.children
	d.children = map[int]*Device{}
	d.cmu.Unlock()
	for _, c := range kids {
		c.release()
	}
	d.once.Do(func() { close(d.gone) })
	if _, err := d.h.command(TRB{Control: cmdDisableSlot<<ctlTypeSh | uint32(d.Slot)<<ctlSlotSh}); err != nil && d.h.Warn != nil {
		d.h.Warn("disable slot %d: %v", d.Slot, err)
	}
	le.PutUint64(d.h.dcbaa[8*d.Slot:], 0)
}
