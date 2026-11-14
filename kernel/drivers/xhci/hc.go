// hc.go brings a controller up (spec §4.2) and runs its rings: the firmware
// lets go of it (USB legacy support), it is reset, given the device context
// array (with scratchpad pages when it asks), the command ring and the
// event ring, and started with interrupter 0 enabled. Interrupt drains the
// event ring and wakes whoever waits on each event's TRB; a command or
// control transfer also drains the ring itself while it waits, so a lost
// interrupt costs time, never a hang. Never more than one command at once.
package xhci

import (
	"errors"
	"fmt"
	"sync"
	"time"
)

// Controller is one xHCI controller.
type Controller struct {
	r                      Regs
	dma                    DMAPage
	opBase, rtBase, dbBase uint32
	ctx                    layout
	Slots, Ports           int
	Version                uint16

	dcbaa []byte
	cmdMu sync.Mutex
	cmd   *Ring
	evMu  sync.Mutex
	ev    *EventRing

	wmu      sync.Mutex
	waits    map[uint64]chan TRB
	PortHint chan int // ports whose status changed (hot plug), never blocking the handler
	Warn     func(format string, a ...any)
}

var errTimeout = errors.New("xhci: timed out")

// New takes the controller from the firmware, resets it and starts it.
func New(r Regs, dma DMAPage) (*Controller, error) {
	h := &Controller{r: r, dma: dma, waits: map[uint64]chan TRB{}, PortHint: make(chan int, 16),
		Warn: func(string, ...any) {}}
	cap0 := r.R32(capLength)
	h.opBase, h.Version = cap0&0xFF, uint16(cap0>>16)
	p1 := r.R32(hcsParams1)
	h.Slots, h.Ports = int(p1&0xFF), int(p1>>24)
	h.rtBase, h.dbBase = r.R32(rtsOff)&^0x1F, r.R32(dbOff)&^3
	hcc := r.R32(hccParams1)
	h.ctx = 32
	if hcc&hccCSZ != 0 {
		h.ctx = 64
	}
	h.takeFromFirmware(hcc >> 16 * 4)
	if err := h.reset(); err != nil {
		return nil, err
	}
	if err := h.setup(); err != nil {
		return nil, err
	}
	return h, nil
}

// wait polls cond every millisecond for up to d.
func wait(d time.Duration, cond func() bool) bool {
	for end := time.Now().Add(d); !cond(); {
		if time.Now().After(end) {
			return false
		}
		time.Sleep(time.Millisecond)
	}
	return true
}

// takeFromFirmware asks the BIOS to give the controller up and turns its
// SMIs off (spec §4.22.1; what Linux does: the reserved bits kept).
func (h *Controller) takeFromFirmware(off uint32) {
	for i := 0; off != 0 && i < 64; i++ {
		v := h.r.R32(off)
		if v&0xFF == xcapLegacy {
			h.r.W32(off, v|legacyOSOwned)
			if !wait(time.Second, func() bool { return h.r.R32(off)&legacyBIOSOwned == 0 }) {
				h.Warn("the firmware kept the controller; taking it")
				h.r.W32(off, h.r.R32(off)&^legacyBIOSOwned)
			}
			h.r.W32(off+legacyCtlSts, h.r.R32(off+legacyCtlSts)&0x000E1FEE|legacySMIOff)
			return
		}
		next := v >> 8 & 0xFF
		if next == 0 {
			return
		}
		off += next * 4
	}
}

func (h *Controller) reset() error {
	if !wait(time.Second, func() bool { return h.op(opUSBSTS)&stsCNR == 0 }) {
		return fmt.Errorf("not ready (USBSTS %#x)", h.op(opUSBSTS))
	}
	h.setOp(opUSBCMD, h.op(opUSBCMD)&^(cmdRun|cmdINTE))
	if !wait(100*time.Millisecond, func() bool { return h.op(opUSBSTS)&stsHalted != 0 }) {
		return fmt.Errorf("did not halt (USBSTS %#x)", h.op(opUSBSTS))
	}
	h.setOp(opUSBCMD, cmdReset)
	time.Sleep(time.Millisecond) // some Intel controllers hang if read at once
	if !wait(time.Second, func() bool { return h.op(opUSBCMD)&cmdReset == 0 && h.op(opUSBSTS)&stsCNR == 0 }) {
		return fmt.Errorf("reset did not finish (USBCMD %#x USBSTS %#x)", h.op(opUSBCMD), h.op(opUSBSTS))
	}
	return nil
}

func (h *Controller) page() ([]byte, uint64, error) {
	pa, mem, err := h.dma()
	return mem, pa, err
}

func (h *Controller) setup() error {
	if h.op(opPageSize)&1 == 0 {
		return fmt.Errorf("no 4 KiB pages (PAGESIZE %#x)", h.op(opPageSize))
	}
	dcbaa, dcbaaPA, err := h.page()
	if err != nil {
		return err
	}
	h.dcbaa = dcbaa
	p2 := h.r.R32(hcsParams2)
	if n := int(p2>>21&0x1F)<<5 | int(p2>>27&0x1F); n > 0 {
		if err := h.scratchpads(n); err != nil {
			return err
		}
	}
	h.setOp(opConfig, uint32(min(h.Slots, 255)))
	h.setOp64(opDCBAAP, dcbaaPA)
	cmd, cmdPA, err := h.page()
	if err != nil {
		return err
	}
	h.cmd = NewRing(cmd, cmdPA, cmdNoOp)
	h.setOp64(opCRCR, cmdPA|crcrRCS)
	ev, evPA, err := h.page()
	if err != nil {
		return err
	}
	erst, erstPA, err := h.page()
	if err != nil {
		return err
	}
	h.ev = NewEventRing(ev, evPA)
	le.PutUint64(erst[0:], evPA)
	le.PutUint32(erst[8:], ringTRBs)
	h.r.W32(h.rtBase+irERSTSZ, 1)
	h.setRT64(irERDP, evPA)
	h.setRT64(irERSTBA, erstPA)
	h.r.W32(h.rtBase+irIMOD, 0)
	h.r.W32(h.rtBase+irIMAN, imanIP|imanIE)
	h.setOp(opUSBCMD, cmdRun|cmdINTE)
	if !wait(100*time.Millisecond, func() bool { return h.op(opUSBSTS)&stsHalted == 0 }) {
		return fmt.Errorf("did not start (USBSTS %#x)", h.op(opUSBSTS))
	}
	return nil
}

// scratchpads gives the controller the n pages it asked for (spec §4.20).
func (h *Controller) scratchpads(n int) error {
	if n > pageBytes/8 {
		return fmt.Errorf("%d scratchpad pages asked for", n)
	}
	arr, arrPA, err := h.page()
	if err != nil {
		return err
	}
	for i := range n {
		_, pa, err := h.page()
		if err != nil {
			return err
		}
		le.PutUint64(arr[8*i:], pa)
	}
	le.PutUint64(h.dcbaa[0:], arrPA)
	return nil
}

// Interrupt is interrupter 0's handler: whether the controller raised it.
func (h *Controller) Interrupt() bool {
	if h.op(opUSBSTS)&stsEINT == 0 && h.r.R32(h.rtBase+irIMAN)&imanIP == 0 {
		return false
	}
	h.setOp(opUSBSTS, stsEINT)
	h.r.W32(h.rtBase+irIMAN, imanIP|imanIE)
	h.drain()
	return true
}

// drain takes every event the controller wrote and hands each on.
func (h *Controller) drain() {
	h.evMu.Lock()
	defer h.evMu.Unlock()
	n := 0
	for t, ok := h.ev.Next(); ok; t, ok = h.ev.Next() {
		h.dispatch(t)
		n++
	}
	if n > 0 {
		h.setRT64(irERDP, h.ev.DequeuePA()|erdpEHB)
	}
}

func (h *Controller) dispatch(t TRB) {
	switch t.typ() {
	case evCommand, evTransfer:
		h.wmu.Lock()
		ch := h.waits[t.Param]
		delete(h.waits, t.Param)
		h.wmu.Unlock()
		if ch != nil {
			ch <- t // buffered: never blocks
		}
	case evPortStatus:
		select {
		case h.PortHint <- int(t.Param >> 24 & 0xFF):
		default:
		}
	case evHostCtrlError:
		h.Warn("host controller error event (code %d, USBSTS %#x)", t.code(), h.op(opUSBSTS))
	}
}

// expect registers a waiter for the event of the TRB at pa.
func (h *Controller) expect(pa uint64) chan TRB {
	ch := make(chan TRB, 1)
	h.wmu.Lock()
	h.waits[pa] = ch
	h.wmu.Unlock()
	return ch
}

func (h *Controller) forget(pa uint64) {
	h.wmu.Lock()
	delete(h.waits, pa)
	h.wmu.Unlock()
}

// await waits for ch's event, draining the event ring every 20 ms
// meanwhile; d == 0 waits for the interrupt alone, for as long as it takes.
func (h *Controller) await(ch chan TRB, pa uint64, d time.Duration) (TRB, error) {
	if d == 0 {
		return <-ch, nil
	}
	tick := time.NewTicker(20 * time.Millisecond)
	defer tick.Stop()
	end := time.After(d)
	for {
		select {
		case t := <-ch:
			return t, nil
		case <-tick.C:
			h.drain()
		case <-end:
			h.forget(pa)
			select {
			case t := <-ch: // it came after all
				return t, nil
			default:
			}
			return TRB{}, errTimeout
		}
	}
}

// command runs one command and returns its completion event.
func (h *Controller) command(t TRB) (TRB, error) {
	h.cmdMu.Lock()
	defer h.cmdMu.Unlock()
	pa := h.cmd.Push(t)[0]
	ch := h.expect(pa)
	h.ring(0, 0)
	ev, err := h.await(ch, pa, 2*time.Second)
	if err != nil {
		return ev, fmt.Errorf("command %d: %w (USBSTS %#x)", t.typ(), err, h.op(opUSBSTS))
	}
	if ev.code() != ccSuccess {
		return ev, fmt.Errorf("command %d: completion code %d", t.typ(), ev.code())
	}
	return ev, nil
}
