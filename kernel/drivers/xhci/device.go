// device.go is an addressed device as package usb sees it: control
// transfers on endpoint 0 (setup, data and status stages; a stall resets
// the endpoint and moves its ring past the transfer) and pipes on the
// endpoints a class driver opens (Configure Endpoint). Data goes through a
// DMA page per endpoint, copied to and from the caller's Go memory.
package xhci

import (
	"errors"
	"fmt"
	"sync"
	"time"

	"burrow/kernel/usb"
)

const controlTimeout = 2 * time.Second

var errGone = errors.New("the device was detached")

// Device is a device with a slot and an address.
type Device struct {
	h       *Controller
	Slot    int
	Port    int
	Speed   usb.Speed
	Desc    usb.DeviceDescriptor
	Config  usb.Config
	Product string

	place place         // where it is: root port, route, TT (W28-S02)
	gone  chan struct{} // closed when the device is detached
	once  sync.Once

	cmu      sync.Mutex // children
	children map[int]*Device
	hub      bool // MakeHub was called
	hubPorts int
	think    int
	multiTT  bool

	mu      sync.Mutex // endpoint 0, the input context
	in      []byte
	inPA    uint64
	ep0     *Ring
	data    []byte
	dataPA  uint64
	mps0    int
	lastDCI int
}

// Control runs a control transfer (spec §4.11.2.2).
func (d *Device) Control(s usb.Setup, data []byte) (int, error) {
	if len(data) > pageBytes || len(data) != int(s.Length) {
		return 0, fmt.Errorf("control transfer of %d bytes (wLength %d)", len(data), s.Length)
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	sb := s.Bytes()
	trt, statusDir := uint32(0), uint32(ctlDirIn)
	if len(data) > 0 {
		trt, statusDir = 2, ctlDirIn
		if s.In() {
			trt, statusDir = 3, 0
		} else {
			copy(d.data, data)
		}
	}
	td := []TRB{{Param: le.Uint64(sb[:]), Status: 8, Control: trbSetup<<ctlTypeSh | ctlIDT | trt<<ctlTRTSh}}
	if len(data) > 0 {
		dir := uint32(0)
		if s.In() {
			dir = ctlDirIn
		}
		td = append(td, TRB{Param: d.dataPA, Status: uint32(len(data)), Control: trbData<<ctlTypeSh | dir | ctlISP | ctlIOC})
	}
	td = append(td, TRB{Control: trbStatus<<ctlTypeSh | statusDir | ctlIOC})
	pas := d.ep0.Push(td...)
	var dataCh chan TRB
	if len(data) > 0 {
		dataCh = d.h.expect(pas[1])
	}
	last := pas[len(pas)-1]
	statusCh := d.h.expect(last)
	d.h.ring(uint32(d.Slot), 1)
	n := 0
	if dataCh != nil {
		ev, err := d.h.await(dataCh, pas[1], controlTimeout)
		if err == nil {
			err = d.result(ev, 1, d.ep0)
		}
		if err != nil {
			d.h.forget(last)
			return 0, fmt.Errorf("request %#x: data stage: %w", s.Request, err)
		}
		n = len(data) - int(ev.Status&0xFFFFFF)
		if s.In() {
			copy(data, d.data[:n])
		}
	}
	ev, err := d.h.await(statusCh, last, controlTimeout)
	if err == nil {
		err = d.result(ev, 1, d.ep0)
	}
	if err != nil {
		return 0, fmt.Errorf("request %#x: status stage: %w", s.Request, err)
	}
	return n, nil
}

// result turns a transfer event into an error; a stall resets the
// endpoint first, so its next transfer runs.
func (d *Device) result(ev TRB, dci int, r *Ring) error {
	switch ev.code() {
	case ccSuccess, ccShortPacket:
		return nil
	case ccStall:
		if err := d.resetEndpoint(dci, r); err != nil {
			return fmt.Errorf("stall, then %w", err)
		}
		return usb.ErrStall
	}
	return fmt.Errorf("completion code %d", ev.code())
}

// resetEndpoint clears a halted endpoint and moves its dequeue pointer to
// the ring's enqueue pointer: what was queued after the stall is dropped
// (spec §4.6.8, §4.6.10).
func (d *Device) resetEndpoint(dci int, r *Ring) error {
	slot := uint32(d.Slot) << ctlSlotSh
	if _, err := d.h.command(TRB{Control: cmdResetEP<<ctlTypeSh | slot | uint32(dci)<<ctlEPSh}); err != nil {
		return err
	}
	_, err := d.h.command(TRB{Param: r.EnqueuePA(), Control: cmdSetDequeue<<ctlTypeSh | slot | uint32(dci)<<ctlEPSh})
	return err
}

// Open configures an endpoint (Configure Endpoint) and returns its pipe.
func (d *Device) Open(ep usb.Endpoint) (usb.Pipe, error) {
	typ := epType(ep)
	if typ == 0 {
		return nil, fmt.Errorf("endpoint %#x: transfer type %d not served", ep.Address, ep.Type())
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	ring, ringPA, err := d.h.page()
	if err != nil {
		return nil, err
	}
	buf, bufPA, err := d.h.page()
	if err != nil {
		return nil, err
	}
	x := dci(ep)
	d.lastDCI = max(d.lastDCI, x)
	l := d.h.ctx
	l.inputControl(d.in, 1<<0|1<<x)
	l.slotContext(d.in, d.slotInfo())
	l.endpointContext(d.in, x, typ, int(ep.MaxPacket), interval(d.Speed, ep), ringPA)
	if _, err := d.h.command(TRB{Param: d.inPA, Control: cmdConfigureEP<<ctlTypeSh | uint32(d.Slot)<<ctlSlotSh}); err != nil {
		return nil, fmt.Errorf("endpoint %#x: %w", ep.Address, err)
	}
	return &pipe{d: d, dci: x, in: ep.In(), ring: NewRing(ring, ringPA, trbNoOp), buf: buf, bufPA: bufPA,
		wait: ep.Type() != usb.XferInterrupt}, nil
}

// pipe is an open interrupt or bulk endpoint.
type pipe struct {
	mu    sync.Mutex
	d     *Device
	dci   int
	in    bool
	ring  *Ring
	buf   []byte
	bufPA uint64
	wait  bool // bounded waits (bulk); an interrupt IN waits for the device
}

// Transfer moves up to one page (spec §4.11.2.1: one Normal TRB).
func (p *pipe) Transfer(b []byte) (int, error) {
	if len(b) > pageBytes {
		return 0, fmt.Errorf("transfer of %d bytes", len(b))
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.in {
		copy(p.buf, b)
	}
	pa := p.ring.Push(TRB{Param: p.bufPA, Status: uint32(len(b)), Control: trbNormal<<ctlTypeSh | ctlISP | ctlIOC})[0]
	ch := p.d.h.expect(pa)
	p.d.h.ring(uint32(p.d.Slot), uint32(p.dci))
	timeout := time.Duration(0)
	if p.wait {
		timeout = 10 * time.Second
	}
	var ev TRB
	var err error
	if timeout == 0 { // an interrupt IN: until a report, or the device goes
		select {
		case ev = <-ch:
		case <-p.d.gone:
			p.d.h.forget(pa)
			return 0, errGone
		}
	} else {
		ev, err = p.d.h.await(ch, pa, timeout)
	}
	if err == nil {
		err = p.d.result(ev, p.dci, p.ring)
	}
	if err != nil {
		return 0, err
	}
	n := len(b) - int(ev.Status&0xFFFFFF)
	if p.in {
		copy(b, p.buf[:n])
	}
	return n, nil
}
