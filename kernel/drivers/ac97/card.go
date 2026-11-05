// card.go - the codec's set-up and the PCM-out ring, as sound.Output.
//
// The ring: the bus master plays descriptors CIV..LVI; the driver fills the
// one after LVI and moves LVI onto it. One descriptor stays free, so a full
// ring never wraps onto the one playing. When the card has played them all
// it halts (DCH); the next buffer restarts it from descriptor 0 after a box
// reset, rather than relying on where a halted card's index points.
package ac97

import (
	"encoding/binary"
	"errors"
	"fmt"
	"sync"
	"time"
)

// DMAPage hands out one zeroed page a device may read: its physical address
// and its bytes (driver.Env.DMAPage, never Go heap: PIT-44).
type DMAPage func() (pa uint64, mem []byte, err error)

var ErrStalled = errors.New("ac97: the card stopped taking samples")

const (
	resetPolls  = 200
	stallLimit  = 5 * time.Second // no buffer freed for this long: give up
	fourGiB     = 1 << 32         // BDBAR and the descriptors hold 32-bit addresses
	pollBackoff = time.Millisecond
)

var le = binary.LittleEndian

type buffer struct {
	pa  uint64
	mem []byte
}

// Card is one AC'97's playback.
type Card struct {
	p     Ports
	vra   bool
	bdl   []byte
	bdlPA uint64
	bufs  [Buffers]buffer

	mu      sync.Mutex
	cond    *sync.Cond
	rate    int
	stage   []byte // card frames waiting for a whole buffer
	head    int    // the next descriptor to fill
	queued  int    // buffers handed to the card and not yet played
	running bool
	changed chan struct{}
}

// New resets the codec, unmutes it, turns VRA on if it has it, and sets
// the ring up in DMA pages below 4 GiB.
func New(p Ports, page DMAPage) (*Card, error) {
	c := &Card{p: p, changed: make(chan struct{}), stage: make([]byte, 0, BufferBytes)}
	c.cond = sync.NewCond(&c.mu)
	if err := c.resetCodec(); err != nil {
		return nil, err
	}
	var err error
	if c.bdlPA, c.bdl, err = low(page); err != nil {
		return nil, err
	}
	for i := range c.bufs {
		if c.bufs[i].pa, c.bufs[i].mem, err = low(page); err != nil {
			return nil, err
		}
	}
	c.resetBox()
	c.rate = c.SetRate(fixedRate)
	return c, nil
}

// low is a DMA page whose address fits the card's 32-bit registers.
func low(page DMAPage) (uint64, []byte, error) {
	pa, mem, err := page()
	if err != nil {
		return 0, nil, err
	}
	if pa+uint64(len(mem)) > fourGiB || len(mem) < BufferBytes {
		return 0, nil, fmt.Errorf("ac97: DMA page at %#x (%d bytes) out of reach", pa, len(mem))
	}
	return pa, mem, nil
}

func (c *Card) resetCodec() error {
	c.p.out32(globCnt, globColdOff)
	ready := false
	for i := 0; i < resetPolls && !ready; i++ {
		if ready = c.p.in32(globSta)&staCodecRdy != 0; !ready {
			time.Sleep(pollBackoff)
		}
	}
	if !ready {
		return errors.New("ac97: the codec never became ready")
	}
	c.p.setMixer(namReset, 0)
	c.p.setMixer(namMaster, volumeMax)
	c.p.setMixer(namPCMOut, volumePCMGain)
	if c.p.mixer(namExtID)&extVRA != 0 {
		c.p.setMixer(namExtCtrl, c.p.mixer(namExtCtrl)|extVRA)
		c.vra = true
	}
	return nil
}

// resetBox stops the PCM-out channel and resets its registers (CIV, LVI,
// status) to descriptor 0 of the list.
func (c *Card) resetBox() {
	c.p.out8(poCR, 0)
	c.p.out8(poCR, crRR)
	for i := 0; i < resetPolls && c.p.in8(poCR)&crRR != 0; i++ {
		time.Sleep(pollBackoff)
	}
	c.p.out32(poBDBAR, uint32(c.bdlPA))
}

// VRA says whether the codec sets its own rate.
func (c *Card) VRA() bool { return c.vra }

// SetRate asks the codec for hz and returns what it took (48000 without VRA).
func (c *Card) SetRate(hz int) int {
	if !c.vra {
		return fixedRate
	}
	hz = max(minRate, min(maxRate, hz))
	c.mu.Lock()
	defer c.mu.Unlock()
	c.p.setMixer(namFrontRate, uint16(hz))
	c.rate = int(c.p.mixer(namFrontRate))
	return c.rate
}

// syncLocked reads where the card is: how many buffers it still holds, or
// that it halted with all of them played.
func (c *Card) syncLocked() {
	if !c.running {
		c.queued = 0
		return
	}
	if c.p.in16(poSR)&srDCH != 0 {
		c.running, c.queued, c.head = false, 0, 0
		return
	}
	civ, lvi := int(c.p.in8(poCIV)), int(c.p.in8(poLVI))
	c.queued = (lvi-civ)&(Buffers-1) + 1
}

// waitLocked sleeps until the card frees a buffer (Interrupt) or the
// deadline; false at the deadline.
func (c *Card) waitLocked(deadline time.Time) bool {
	left := time.Until(deadline)
	if left <= 0 {
		return false
	}
	t := time.AfterFunc(left, func() { c.mu.Lock(); c.cond.Broadcast(); c.mu.Unlock() })
	c.cond.Wait()
	t.Stop()
	return true
}

// roomLocked waits for a free descriptor.
func (c *Card) roomLocked() error {
	deadline := time.Now().Add(stallLimit)
	for c.syncLocked(); c.queued >= Buffers-1; c.syncLocked() {
		if !c.waitLocked(deadline) {
			return ErrStalled
		}
	}
	return nil
}

// submitLocked hands the staged frames to the card as one buffer. A card
// that halted (everything played) restarts from descriptor 0; one that
// halts just as LVI moves is caught by the check after and restarted.
func (c *Card) submitLocked() {
	for {
		c.syncLocked()
		restart := !c.running
		if restart {
			c.resetBox()
			c.head = 0
		}
		i := c.head
		copy(c.bufs[i].mem, c.stage)
		d := c.bdl[i*descBytes:]
		le.PutUint32(d, uint32(c.bufs[i].pa))
		le.PutUint16(d[4:], uint16(len(c.stage)/2)) // samples, 16 bits each
		le.PutUint16(d[6:], descIOC)
		c.p.out8(poLVI, uint8(i)) // after the descriptor: port I/O orders it
		if restart {
			c.p.out8(poCR, crRun|crIOCE|crLVBI)
			c.running = true
		} else if c.p.in16(poSR)&srDCH != 0 {
			c.running = false // it halted before it saw the new LVI: again, from 0
			continue
		}
		c.head = (i + 1) % Buffers
		c.queued++
		c.stage = c.stage[:0]
		return
	}
}

// Write queues card frames (16-bit stereo), blocking while the ring is full.
func (c *Card) Write(p []byte) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	n := 0
	for len(p) > 0 {
		k := min(len(p), BufferBytes-len(c.stage))
		c.stage = append(c.stage, p[:k]...)
		p, n = p[k:], n+k
		if len(c.stage) == BufferBytes {
			if err := c.roomLocked(); err != nil {
				return n, err
			}
			c.submitLocked()
		}
	}
	return n, nil
}

// Drain hands over a partial buffer and waits until the card has played
// everything (or stalls).
func (c *Card) Drain() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.stage) > 0 && c.roomLocked() == nil {
		c.submitLocked()
	}
	deadline := time.Now().Add(stallLimit)
	for c.syncLocked(); c.running; c.syncLocked() {
		if !c.waitLocked(deadline) {
			return
		}
	}
}

// Reset stops playback and drops everything queued.
func (c *Card) Reset() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.resetBox()
	c.running, c.queued, c.head = false, 0, 0
	c.stage = c.stage[:0]
	c.wakeLocked()
}

// Space: free descriptors, how many there are for a writer, bytes in one.
func (c *Card) Space() (free, total, size int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.syncLocked()
	return Buffers - 1 - c.queued, Buffers - 1, BufferBytes
}

// Delay is the bytes written and not yet played.
func (c *Card) Delay() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.syncLocked()
	if !c.running {
		return len(c.stage)
	}
	return (c.queued-1)*BufferBytes + int(c.p.in16(poPICB))*2 + len(c.stage)
}

// Changed is closed when the card frees a buffer.
func (c *Card) Changed() <-chan struct{} {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.changed
}

func (c *Card) wakeLocked() {
	c.cond.Broadcast()
	close(c.changed)
	c.changed = make(chan struct{})
}

// Interrupt serves the card on its (maybe shared) line and says whether it
// raised the interrupt: a buffer completed, the last one did, or the FIFO
// ran dry.
func (c *Card) Interrupt() (mine bool) {
	sr := c.p.in16(poSR)
	if sr&srIRQs == 0 {
		return false
	}
	c.p.out16(poSR, sr&srIRQs) // write-1-to-clear
	c.mu.Lock()
	c.syncLocked()
	c.wakeLocked()
	c.mu.Unlock()
	return true
}
