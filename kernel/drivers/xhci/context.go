// context.go fills input contexts (spec §6.2): the input control context,
// the slot context and endpoint contexts, 32 or 64 bytes each (CSZ).
// Pure: it writes into a page's bytes and never reads the device's output
// context. An endpoint's context index (DCI) is 2*number + 1 for IN, 2*number
// for OUT; endpoint 0 is DCI 1.
package xhci

import "burrow/kernel/usb"

// Endpoint types in an endpoint context (spec Table 6-9).
const (
	epControl      = 4
	epBulkOut      = 2
	epBulkIn       = 6
	epInterruptOut = 3
	epInterruptIn  = 7
)

// dci is an endpoint's device context index.
func dci(ep usb.Endpoint) int {
	if ep.In() {
		return 2*ep.Number() + 1
	}
	return 2 * ep.Number()
}

// layout is the context size: 32 bytes, 64 with CSZ.
type layout int

// in returns input context entry i: 0 is the input control context, 1 the
// slot, 1+DCI an endpoint.
func (l layout) in(page []byte, i int) []byte { return page[i*int(l) : (i+1)*int(l)] }

// slotInfo is what a slot context says about a device (spec §6.2.2):
// where it is (its root-hub port and the route string of hub ports below
// it, four bits a tier), its speed, the last endpoint context in use, the
// transaction translator a low- or full-speed device behind a high-speed
// hub talks through, and, for a hub, its ports and TT (W28-S02).
type slotInfo struct {
	rootPort       int
	route          uint32
	speed          usb.Speed
	lastDCI        int
	ttSlot, ttPort int
	hub            bool
	multiTT        bool
	ports          int
	thinkTime      int
}

// slotContext writes the slot context from s.
func (l layout) slotContext(page []byte, s slotInfo) {
	c := l.in(page, 1)
	dw0 := s.route&0xFFFFF | uint32(s.speed)<<20 | uint32(s.lastDCI)<<27
	if s.multiTT {
		dw0 |= 1 << 25
	}
	if s.hub {
		dw0 |= 1 << 26
	}
	le.PutUint32(c[0:], dw0)
	le.PutUint32(c[4:], uint32(s.rootPort)<<16|uint32(s.ports)<<24)
	le.PutUint32(c[8:], uint32(s.ttSlot)|uint32(s.ttPort)<<8|uint32(s.thinkTime)<<16)
	le.PutUint32(c[12:], 0)
}

// childRoute is the route string of the device on port of a hub at depth
// with route parent: tier depth+1's nibble is the port (15 at most).
func childRoute(parent uint32, depth, port int) uint32 {
	return parent | uint32(min(port, 15))<<(4*depth)
}

// endpointContext writes endpoint dci: its type, packet size, interval
// exponent and ring (dequeue pointer with cycle 1).
func (l layout) endpointContext(page []byte, d, typ, maxPacket, interval int, ring uint64) {
	e := l.in(page, 1+d)
	clear(e)
	le.PutUint32(e[0:], uint32(interval)<<16)
	const cerr = 3 // retries of a failed transaction
	le.PutUint32(e[4:], cerr<<1|uint32(typ)<<3|uint32(maxPacket)<<16)
	le.PutUint64(e[8:], ring|1)
	avg := 8 // average TRB length: setup packets
	if typ != epControl {
		avg = maxPacket
	}
	le.PutUint32(e[16:], uint32(avg)|uint32(maxPacket)<<16) // max ESIT payload: one packet
}

// inputControl sets which contexts a command adds (bit i: entry i).
func (l layout) inputControl(page []byte, add uint32) {
	c := l.in(page, 0)
	clear(c)
	le.PutUint32(c[4:], add)
}

// defaultMaxPacket0 is endpoint 0's packet size before the device
// descriptor says (USB 2.0 §5.5.3; USB 3.2 §9.6.1).
func defaultMaxPacket0(s usb.Speed) int {
	switch s {
	case usb.SpeedLow, usb.SpeedFull:
		return 8
	case usb.SpeedHigh:
		return 64
	}
	return 512
}

// maxPacket0 reads bMaxPacketSize0: an exponent from USB 3 on.
func maxPacket0(s usb.Speed, b uint8) int {
	if s == usb.SpeedSuper {
		return 1 << b
	}
	return int(b)
}

// interval is an endpoint context's interval exponent (2^n x 125 us) for a
// descriptor's bInterval (spec §6.2.3.6): frames for full and low speed,
// already an exponent + 1 at high and super speed.
func interval(s usb.Speed, ep usb.Endpoint) int {
	if ep.Type() != usb.XferInterrupt {
		return 0
	}
	if s == usb.SpeedHigh || s == usb.SpeedSuper {
		return min(max(int(ep.Interval), 1), 16) - 1
	}
	n := 3 // 1 ms = 2^3 x 125 us
	for f := max(int(ep.Interval), 1); f > 1; f >>= 1 {
		n++
	}
	return min(n, 10)
}

// epType is an endpoint context's type for a descriptor.
func epType(ep usb.Endpoint) int {
	switch {
	case ep.Type() == usb.XferBulk && ep.In():
		return epBulkIn
	case ep.Type() == usb.XferBulk:
		return epBulkOut
	case ep.Type() == usb.XferInterrupt && ep.In():
		return epInterruptIn
	case ep.Type() == usb.XferInterrupt:
		return epInterruptOut
	}
	return 0
}
