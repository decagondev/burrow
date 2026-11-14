// ring.go is the TRB rings (spec §4.9): a producer ring (the command ring,
// each endpoint's transfer ring) of one page closed by a Link TRB that
// toggles the cycle bit, and the event ring the controller produces into.
// Pure: a ring is its page's bytes and physical address. A TRB's control
// word, which holds the cycle bit, is written last and atomically, so the
// controller never sees a half-written TRB as its own; a transfer
// descriptor never straddles the Link TRB (the gap is filled with no-ops).
package xhci

import (
	"sync/atomic"
	"unsafe"
)

const (
	trbBytes = 16
	ringTRBs = pageBytes / trbBytes // the last one is the Link TRB
)

// TRB types (spec §6.4.6).
const (
	trbNormal       = 1
	trbSetup        = 2
	trbData         = 3
	trbStatus       = 4
	trbLink         = 6
	trbNoOp         = 8
	cmdEnableSlot   = 9
	cmdAddress      = 11
	cmdConfigureEP  = 12
	cmdEvaluate     = 13
	cmdResetEP      = 14
	cmdSetDequeue   = 16
	cmdNoOp         = 23
	evTransfer      = 32
	evCommand       = 33
	evPortStatus    = 34
	evHostCtrlError = 37
)

// Control word bits.
const (
	ctlCycle   = 1 << 0
	ctlToggle  = 1 << 1 // Link: toggle the cycle bit
	ctlISP     = 1 << 2 // interrupt on short packet
	ctlChain   = 1 << 4
	ctlIOC     = 1 << 5
	ctlIDT     = 1 << 6 // immediate data (the setup packet)
	ctlTypeSh  = 10
	ctlDirIn   = 1 << 16 // Data, Status stages
	ctlTRTSh   = 16      // Setup: transfer type 0 none, 2 OUT, 3 IN
	ctlSlotSh  = 24      // commands, events
	ctlEPSh    = 16      // events, Reset Endpoint: the endpoint's DCI
	statusCCSh = 24      // events: the completion code
)

// Completion codes (spec §6.4.5).
const (
	ccSuccess     = 1
	ccTransaction = 4
	ccStall       = 6
	ccShortPacket = 13
)

// TRB is one transfer request block.
type TRB struct {
	Param   uint64
	Status  uint32
	Control uint32
}

func (t TRB) typ() int      { return int(t.Control>>ctlTypeSh) & 0x3F }
func (t TRB) code() int     { return int(t.Status >> statusCCSh) }
func (t TRB) slot() int     { return int(t.Control >> ctlSlotSh) }
func (t TRB) endpoint() int { return int(t.Control>>ctlEPSh) & 0x1F }

func control32(mem []byte, i int) *uint32 {
	return (*uint32)(unsafe.Pointer(&mem[i*trbBytes+12]))
}

// Ring is a producer ring.
type Ring struct {
	mem   []byte
	pa    uint64
	enq   int
	cycle uint32
	noop  int // the no-op type that pads this ring (trbNoOp or cmdNoOp)
}

// NewRing lays a ring on a zeroed page; its Link TRB points at its start.
func NewRing(mem []byte, pa uint64, noop int) *Ring {
	r := &Ring{mem: mem[:pageBytes], pa: pa, cycle: 1, noop: noop}
	le.PutUint64(r.mem[(ringTRBs-1)*trbBytes:], pa)
	return r
}

// DequeuePA is where the controller starts: the ring's base, cycle 1.
func (r *Ring) DequeuePA() uint64 { return r.pa | 1 }

// EnqueuePA is the next TRB to be written, with the producer's cycle: Set
// TR Dequeue Pointer's argument after a stall.
func (r *Ring) EnqueuePA() uint64 { return r.pa + uint64(r.enq*trbBytes) | uint64(r.cycle) }

func (r *Ring) put(t TRB) uint64 {
	off := r.enq * trbBytes
	le.PutUint64(r.mem[off:], t.Param)
	le.PutUint32(r.mem[off+8:], t.Status)
	atomic.StoreUint32(control32(r.mem, r.enq), t.Control&^ctlCycle|r.cycle)
	pa := r.pa + uint64(off)
	if r.enq++; r.enq == ringTRBs-1 {
		link := uint32(trbLink<<ctlTypeSh | ctlToggle)
		atomic.StoreUint32(control32(r.mem, r.enq), link|r.cycle)
		r.enq, r.cycle = 0, r.cycle^1
	}
	return pa
}

// Push writes a transfer descriptor (or one command) and returns each
// TRB's physical address, the key of its event. The caller rings the
// doorbell.
func (r *Ring) Push(td ...TRB) []uint64 {
	if len(td) > ringTRBs/2 {
		panic("xhci: transfer descriptor longer than half a ring")
	}
	for r.enq+len(td) > ringTRBs-1 {
		r.put(TRB{Control: uint32(r.noop) << ctlTypeSh})
	}
	pas := make([]uint64, len(td))
	for i, t := range td {
		pas[i] = r.put(t)
	}
	return pas
}

// EventRing is the one-segment event ring of interrupter 0.
type EventRing struct {
	mem   []byte
	pa    uint64
	deq   int
	cycle uint32
}

func NewEventRing(mem []byte, pa uint64) *EventRing {
	return &EventRing{mem: mem[:pageBytes], pa: pa, cycle: 1}
}

// Next takes the next event the controller wrote, if there is one.
func (e *EventRing) Next() (TRB, bool) {
	ctl := atomic.LoadUint32(control32(e.mem, e.deq))
	if ctl&ctlCycle != e.cycle {
		return TRB{}, false
	}
	off := e.deq * trbBytes
	t := TRB{Param: le.Uint64(e.mem[off:]), Status: le.Uint32(e.mem[off+8:]), Control: ctl}
	if e.deq++; e.deq == ringTRBs {
		e.deq, e.cycle = 0, e.cycle^1
	}
	return t, true
}

// DequeuePA is what ERDP gets after the events are taken.
func (e *EventRing) DequeuePA() uint64 { return e.pa + uint64(e.deq*trbBytes) }
