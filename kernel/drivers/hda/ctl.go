// ctl.go is the controller (spec §3.3, §4.2-4.4): a reset that brings the
// link up and says which codecs answered (STATESTS), and the command
// rings: verbs go out through the CORB and responses come back through the
// RIRB, both in one DMA page, polled (one verb at a time; each response's
// RIRBSTS flag is cleared, the controller interrupt stays off, unsolicited
// responses are skipped). A codec that
// does not answer in time is an error, never a hang.
package hda

import (
	"errors"
	"fmt"
	"sync"
	"time"
)

var (
	resetWait    = 100 * time.Millisecond
	codecSettle  = time.Millisecond // a codec reports itself within 521 us of the reset (§4.3)
	verbWait     = 500 * time.Millisecond
	ringWait     = 10 * time.Millisecond
	errNoCodec   = errors.New("hda: no codec answered the reset")
	errVerbStall = errors.New("hda: no response")
)

// Controller is a started HD Audio controller.
type Controller struct {
	r      Regs
	oss    int // output streams
	iss    int // input streams (the output streams' descriptors come after theirs)
	codecs uint16

	cmdMu   sync.Mutex
	ring    []byte // CORB at 0, RIRB at rirbOffset
	entries int
	corbWP  int
	rirbRP  int
}

// NewController resets the controller behind r and starts its rings in a
// page from page.
func NewController(r Regs, page DMAPage) (*Controller, error) {
	c := &Controller{r: r}
	gcap := r.R16(regGCAP)
	c.oss, c.iss = int(gcap>>12)&0xF, int(gcap>>8)&0xF
	if c.oss == 0 {
		return nil, fmt.Errorf("hda: no output streams (GCAP %#x)", gcap)
	}
	if err := c.reset(); err != nil {
		return nil, err
	}
	if c.codecs = r.R16(regSTATESTS) & 0x7FFF; c.codecs == 0 {
		return nil, errNoCodec
	}
	r.W16(regSTATESTS, c.codecs) // write 1 to clear
	pa, mem, err := page()
	if err != nil {
		return nil, err
	}
	if pa+pageBytes > fourGiBPA && gcap&1 == 0 {
		return nil, fmt.Errorf("hda: ring page at %#x above 4 GiB without 64-bit addressing", pa)
	}
	c.ring = mem
	c.startRings(pa)
	return c, nil
}

// reset puts the controller in reset and takes it out again (§4.2.2).
func (c *Controller) reset() error {
	c.r.W32(regINTCTL, 0)
	c.r.W8(regCORBCTL, 0)
	c.r.W8(regRIRBCTL, 0)
	c.r.W32(regGCTL, c.r.R32(regGCTL)&^gctlCRST)
	if !waitFor(resetWait, func() bool { return c.r.R32(regGCTL)&gctlCRST == 0 }) {
		return errors.New("hda: the controller did not enter reset")
	}
	time.Sleep(codecSettle)
	c.r.W32(regGCTL, c.r.R32(regGCTL)|gctlCRST)
	if !waitFor(resetWait, func() bool { return c.r.R32(regGCTL)&gctlCRST != 0 }) {
		return errors.New("hda: the controller did not leave reset")
	}
	time.Sleep(codecSettle)
	return nil
}

// ringSize is the largest of 256 or 16 entries the size register allows.
func ringSize(sz uint8) (uint8, int) {
	switch {
	case sz&ringCap256 != 0:
		return ringSize256, 256
	case sz&ringCap16 != 0:
		return ringSize16, 16
	}
	return 0, 2
}

func (c *Controller) startRings(pa uint64) {
	r := c.r
	code, n := ringSize(r.R8(regCORBSIZE))
	c.entries = n
	r.W32(regCORBLBASE, uint32(pa+corbOffset))
	r.W32(regCORBUBASE, uint32((pa+corbOffset)>>32))
	r.W8(regCORBSIZE, code)
	r.W16(regCORBRP, corbRPRST) // the read pointer back to 0; some controllers show RST until it is cleared
	waitFor(ringWait, func() bool { return r.R16(regCORBRP)&corbRPRST != 0 })
	r.W16(regCORBRP, 0)
	waitFor(ringWait, func() bool { return r.R16(regCORBRP)&corbRPRST == 0 })
	r.W16(regCORBWP, 0)
	r.W32(regRIRBLBASE, uint32(pa+rirbOffset))
	r.W32(regRIRBUBASE, uint32((pa+rirbOffset)>>32))
	r.W8(regRIRBSIZE, code)
	r.W16(regRIRBWP, rirbWPRST)
	r.W16(regRINTCNT, 1)
	r.W8(regCORBCTL, ringRun)
	// RINTCTL on: RIRBSTS then flags each response, and clearing the flag
	// lets the controller go on to the next verb (QEMU stops after RINTCNT
	// responses until it is cleared). The controller interrupt stays off:
	// INTCTL's CIE is never set.
	r.W8(regRIRBCTL, ringRun|rirbRINTCTL)
	c.corbWP, c.rirbRP = 0, 0
}

// Verb sends one verb and returns the codec's response.
func (c *Controller) Verb(cad, nid int, verb uint32) (uint32, error) {
	c.cmdMu.Lock()
	defer c.cmdMu.Unlock()
	cmd := uint32(cad)<<28 | uint32(nid&0x7F)<<20 | verb&0xFFFFF
	c.corbWP = (c.corbWP + 1) % c.entries
	le.PutUint32(c.ring[corbOffset+4*c.corbWP:], cmd)
	c.r.W16(regCORBWP, uint16(c.corbWP))
	deadline := time.Now().Add(verbWait)
	for {
		for wp := int(c.r.R16(regRIRBWP) & 0xFF); c.rirbRP != wp%c.entries; {
			c.rirbRP = (c.rirbRP + 1) % c.entries
			e := c.ring[rirbOffset+8*c.rirbRP:]
			resp, ext := le.Uint32(e), le.Uint32(e[4:])
			if ext&(1<<4) == 0 && int(ext&0xF) == cad { // not unsolicited, from this codec
				c.r.W8(regRIRBSTS, 0xFF)
				return resp, nil
			}
		}
		if time.Now().After(deadline) {
			return 0, fmt.Errorf("%w from codec %d node %d to verb %#x", errVerbStall, cad, nid, verb)
		}
		time.Sleep(10 * time.Microsecond)
	}
}

// Codecs lists the codec addresses that answered the reset.
func (c *Controller) Codecs() []int {
	var out []int
	for i := 0; i < 15; i++ {
		if c.codecs&(1<<i) != 0 {
			out = append(out, i)
		}
	}
	return out
}

// OutputBase is output stream i's descriptor offset.
func (c *Controller) OutputBase(i int) uint32 {
	return streamBase + streamBytes*uint32(c.iss+i)
}

// OutputIndex is output stream i's bit in INTCTL and INTSTS.
func (c *Controller) OutputIndex(i int) int { return c.iss + i }

func waitFor(d time.Duration, ok func() bool) bool {
	for end := time.Now().Add(d); ; time.Sleep(50 * time.Microsecond) {
		if ok() {
			return true
		}
		if time.Now().After(end) {
			return false
		}
	}
}
