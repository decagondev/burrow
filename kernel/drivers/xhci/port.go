// port.go enumerates the device on a root-hub port (spec §4.3): reset the
// port (a USB 2 port; a USB 3 port is enabled by its link training), take
// a slot, give the device an address with endpoint 0 at its speed's
// default packet size, correct the size from the first 8 bytes of the
// device descriptor, then read the whole descriptor and configuration.
// USB's settle times are real milliseconds (WAVE-27's risks): they are
// slept, never skipped.
package xhci

import (
	"errors"
	"fmt"
	"time"

	"burrow/kernel/usb"
)

var errNoDevice = errors.New("nothing connected")

const (
	resetRecovery   = 10 * time.Millisecond // USB 2.0 §7.1.7.5, TRSTRCY
	addressRecovery = 2 * time.Millisecond  // §9.2.6.3, SetAddress
	powerSettle     = 20 * time.Millisecond
)

// Connected says whether a device is on the port.
func (h *Controller) Connected(port int) bool { return h.portsc(port)&portCCS != 0 }

// resetPort enables the port and returns the device's speed.
func (h *Controller) resetPort(port int) (usb.Speed, error) {
	if h.portsc(port)&portPP == 0 {
		h.portWrite(port, portPP, 0)
		time.Sleep(powerSettle)
	}
	sc := h.portsc(port)
	if sc&portCCS == 0 {
		return 0, errNoDevice
	}
	h.portWrite(port, 0, sc&portChanges)
	if sc&portPED == 0 {
		h.portWrite(port, portPR, 0)
		if !wait(500*time.Millisecond, func() bool { return h.portsc(port)&portPRC != 0 }) {
			return 0, fmt.Errorf("reset did not finish (PORTSC %#x)", h.portsc(port))
		}
		h.portWrite(port, 0, portPRC)
		if sc = h.portsc(port); sc&portPED == 0 {
			return 0, fmt.Errorf("not enabled after reset (PORTSC %#x)", sc)
		}
	}
	time.Sleep(resetRecovery)
	return usb.Speed(h.portsc(port) >> portSpeedShift & portSpeedMask), nil
}

// Enumerate addresses the device on port and reads its descriptors.
func (h *Controller) Enumerate(port int) (*Device, error) {
	speed, err := h.resetPort(port)
	if err != nil {
		return nil, err
	}
	ev, err := h.command(TRB{Control: cmdEnableSlot << ctlTypeSh})
	if err != nil {
		return nil, err
	}
	d, err := h.newDevice(ev.slot(), port, speed)
	if err != nil {
		return nil, err
	}
	if err := d.address(); err != nil {
		return nil, err
	}
	head := make([]byte, 8)
	if _, err := usb.GetDescriptor(d, usb.DescDevice, 0, 0, head); err != nil {
		return nil, fmt.Errorf("device descriptor: %w", err)
	}
	dd, _ := usb.ParseDevice(head) // 8 bytes: only MaxPacket0
	if m := maxPacket0(speed, dd.MaxPacket0); m != d.mps0 && m >= 8 {
		if err := d.setMaxPacket0(m); err != nil {
			return nil, err
		}
	}
	full := make([]byte, 18)
	n, err := usb.GetDescriptor(d, usb.DescDevice, 0, 0, full)
	if err != nil {
		return nil, fmt.Errorf("device descriptor: %w", err)
	}
	if d.Desc, err = usb.ParseDevice(full[:n]); err != nil {
		return nil, err
	}
	if d.Config, err = usb.ReadConfig(d); err != nil {
		return nil, fmt.Errorf("configuration: %w", err)
	}
	d.Product = usb.ReadString(d, d.Desc.IProduct)
	return d, nil
}

func (h *Controller) newDevice(slot, port int, speed usb.Speed) (*Device, error) {
	if slot <= 0 || slot > h.Slots {
		return nil, fmt.Errorf("slot %d out of range", slot)
	}
	d := &Device{h: h, Slot: slot, Port: port, Speed: speed, lastDCI: 1, mps0: defaultMaxPacket0(speed)}
	var outPA, ringPA uint64
	var err error
	var ring []byte
	if _, outPA, err = h.page(); err != nil {
		return nil, err
	}
	if d.in, d.inPA, err = h.page(); err != nil {
		return nil, err
	}
	if ring, ringPA, err = h.page(); err != nil {
		return nil, err
	}
	if d.data, d.dataPA, err = h.page(); err != nil {
		return nil, err
	}
	d.ep0 = NewRing(ring, ringPA, trbNoOp)
	le.PutUint64(h.dcbaa[8*slot:], outPA)
	return d, nil
}

// address sends Address Device: the slot and endpoint 0 (spec §4.6.5).
func (d *Device) address() error {
	l := d.h.ctx
	l.inputControl(d.in, 1<<0|1<<1)
	l.slotContext(d.in, d.Port, d.Speed, 1)
	l.endpointContext(d.in, 1, epControl, d.mps0, 0, d.ep0.pa)
	_, err := d.h.command(TRB{Param: d.inPA, Control: cmdAddress<<ctlTypeSh | uint32(d.Slot)<<ctlSlotSh})
	if err != nil {
		return fmt.Errorf("address device: %w", err)
	}
	time.Sleep(addressRecovery)
	return nil
}

// setMaxPacket0 corrects endpoint 0's packet size (Evaluate Context).
func (d *Device) setMaxPacket0(m int) error {
	l := d.h.ctx
	l.inputControl(d.in, 1<<1)
	l.endpointContext(d.in, 1, epControl, m, 0, d.ep0.pa)
	if _, err := d.h.command(TRB{Param: d.inPA, Control: cmdEvaluate<<ctlTypeSh | uint32(d.Slot)<<ctlSlotSh}); err != nil {
		return fmt.Errorf("endpoint 0 packet size %d: %w", m, err)
	}
	d.mps0 = m
	return nil
}
