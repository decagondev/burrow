package xhci

import (
	"testing"

	"burrow/kernel/usb"
)

const ringPA = 0x10000

func trbAt(mem []byte, i int) TRB {
	off := i * trbBytes
	return TRB{Param: le.Uint64(mem[off:]), Status: le.Uint32(mem[off+8:]), Control: le.Uint32(mem[off+12:])}
}

func TestRingWrapsThroughLink(t *testing.T) {
	mem := make([]byte, pageBytes)
	r := NewRing(mem, ringPA, trbNoOp)
	if link := trbAt(mem, ringTRBs-1); link.Param != ringPA {
		t.Fatalf("link points at %#x", link.Param)
	}
	for i := range ringTRBs - 1 {
		pa := r.Push(TRB{Param: uint64(i), Control: trbNormal << ctlTypeSh})[0]
		if pa != ringPA+uint64(i*trbBytes) {
			t.Fatalf("TRB %d at %#x", i, pa)
		}
	}
	link := trbAt(mem, ringTRBs-1)
	if link.typ() != trbLink || link.Control&ctlCycle != 1 || link.Control&ctlToggle == 0 {
		t.Fatalf("link %#x after the first lap", link.Control)
	}
	if r.enq != 0 || r.cycle != 0 || r.EnqueuePA() != ringPA {
		t.Fatalf("after the lap: enq %d cycle %d", r.enq, r.cycle)
	}
	r.Push(TRB{Control: trbNormal << ctlTypeSh})
	if c := trbAt(mem, 0).Control; c&ctlCycle != 0 || c&ctlChain != 0 {
		t.Fatalf("second lap's first TRB control %#x", c)
	}
}

func TestRingKeepsTransferDescriptorsWhole(t *testing.T) {
	mem := make([]byte, pageBytes)
	r := NewRing(mem, ringPA, trbNoOp)
	for range ringTRBs - 3 { // two slots left before the link
		r.Push(TRB{Control: trbNormal << ctlTypeSh})
	}
	pas := r.Push(TRB{Control: trbSetup << ctlTypeSh}, TRB{Control: trbData << ctlTypeSh}, TRB{Control: trbStatus << ctlTypeSh})
	if pas[0] != ringPA || pas[2] != ringPA+2*trbBytes {
		t.Fatalf("a control transfer at %#x..%#x, not the ring's start", pas[0], pas[2])
	}
	for _, i := range []int{ringTRBs - 3, ringTRBs - 2} {
		if trb := trbAt(mem, i); trb.typ() != trbNoOp || trb.Control&ctlCycle != 1 {
			t.Fatalf("slot %d: %#x, want a no-op of the first lap", i, trb.Control)
		}
	}
	if trb := trbAt(mem, 0); trb.typ() != trbSetup || trb.Control&ctlCycle != 0 {
		t.Fatalf("setup %#x", trb.Control)
	}
}

func TestEventRing(t *testing.T) {
	mem := make([]byte, pageBytes)
	e := NewEventRing(mem, ringPA)
	if _, ok := e.Next(); ok {
		t.Fatal("an event in an empty ring")
	}
	put := func(i int, cycle uint32, param uint64) {
		le.PutUint64(mem[i*trbBytes:], param)
		le.PutUint32(mem[i*trbBytes+12:], evCommand<<ctlTypeSh|5<<ctlSlotSh|cycle)
	}
	for i := range ringTRBs {
		put(i, 1, uint64(i))
	}
	for i := range ringTRBs {
		ev, ok := e.Next()
		if !ok || ev.Param != uint64(i) || ev.typ() != evCommand || ev.slot() != 5 {
			t.Fatalf("event %d: %+v %v", i, ev, ok)
		}
	}
	if _, ok := e.Next(); ok || e.DequeuePA() != ringPA {
		t.Fatal("the second lap's stale events were taken")
	}
	put(0, 0, 77)
	if ev, ok := e.Next(); !ok || ev.Param != 77 {
		t.Fatal("the second lap's first event was missed")
	}
}

func TestContexts(t *testing.T) {
	for _, l := range []layout{32, 64} {
		page := make([]byte, pageBytes)
		ep := usb.Endpoint{Address: 0x81, Attributes: usb.XferInterrupt, MaxPacket: 8, Interval: 10}
		x := dci(ep)
		l.inputControl(page, 1|1<<x)
		l.slotContext(page, slotInfo{rootPort: 3, speed: usb.SpeedFull, lastDCI: x})
		l.endpointContext(page, x, epType(ep), 8, interval(usb.SpeedFull, ep), 0x5000)
		if x != 3 || le.Uint32(page[4:]) != 1|1<<3 {
			t.Fatalf("%d: dci %d, add flags %#x", l, x, le.Uint32(page[4:]))
		}
		s := page[int(l):]
		if le.Uint32(s) != 1<<20|3<<27 || le.Uint32(s[4:]) != 3<<16 {
			t.Fatalf("%d: slot %#x %#x", l, le.Uint32(s), le.Uint32(s[4:]))
		}
		e := page[int(l)*(1+x):]
		if le.Uint32(e) != 6<<16 || le.Uint32(e[4:]) != 3<<1|epInterruptIn<<3|8<<16 || le.Uint64(e[8:]) != 0x5001 {
			t.Fatalf("%d: endpoint %#x %#x %#x", l, le.Uint32(e), le.Uint32(e[4:]), le.Uint64(e[8:]))
		}
	}
}

func TestIntervalsAndPacketSizes(t *testing.T) {
	cases := []struct {
		s        usb.Speed
		interval uint8
		want     int
	}{
		{usb.SpeedFull, 1, 3}, {usb.SpeedFull, 10, 6}, {usb.SpeedLow, 255, 10},
		{usb.SpeedHigh, 4, 3}, {usb.SpeedSuper, 1, 0}, {usb.SpeedHigh, 0, 0},
	}
	for _, c := range cases {
		ep := usb.Endpoint{Address: 0x81, Attributes: usb.XferInterrupt, Interval: c.interval}
		if got := interval(c.s, ep); got != c.want {
			t.Errorf("%v bInterval %d: %d, want %d", c.s, c.interval, got, c.want)
		}
	}
	if interval(usb.SpeedHigh, usb.Endpoint{Attributes: usb.XferBulk}) != 0 {
		t.Error("bulk endpoints have no interval")
	}
	if maxPacket0(usb.SpeedSuper, 9) != 512 || maxPacket0(usb.SpeedFull, 64) != 64 || defaultMaxPacket0(usb.SpeedHigh) != 64 {
		t.Error("endpoint 0 packet sizes")
	}
	if epType(usb.Endpoint{Address: 0x02, Attributes: usb.XferBulk}) != epBulkOut ||
		epType(usb.Endpoint{Address: 0x81, Attributes: usb.XferBulk}) != epBulkIn {
		t.Error("bulk endpoint types")
	}
}

func TestRegBytes(t *testing.T) {
	caps := make(mmio, pageBytes)
	le.PutUint32(caps[capLength:], 0x40)
	le.PutUint32(caps[hcsParams1:], 8<<24|64)
	le.PutUint32(caps[rtsOff:], 0x1000)
	le.PutUint32(caps[dbOff:], 0x2000)
	if n := regBytes(caps); n != 0x3000 {
		t.Fatalf("%#x", n)
	}
}
