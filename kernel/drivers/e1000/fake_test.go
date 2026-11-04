package e1000

// fakeCard plays an 82540EM for host tests: a register file, an EEPROM
// holding the MAC, and DMA into fake physical pages. It walks the transmit
// ring when the tail moves and fills receive descriptors when the test
// delivers a frame, raising its interrupt like the real card.

import (
	"encoding/binary"
	"sync"
	"sync/atomic"
	"unsafe"

	"burrow/kernel/haltest"
)

type fakeMem struct {
	mu    sync.Mutex
	pages map[uint64][]byte
	next  uint64
}

func newFakeMem() *fakeMem { return &fakeMem{pages: map[uint64][]byte{}, next: 0x10_0000} }

func (m *fakeMem) page() (uint64, []byte, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	pa := m.next
	m.next += PageBytes
	m.pages[pa] = make([]byte, PageBytes)
	return pa, m.pages[pa], nil
}

// at is the memory at physical address pa (inside one page).
func (m *fakeMem) at(pa uint64, n int) []byte {
	m.mu.Lock()
	defer m.mu.Unlock()
	base := pa &^ (PageBytes - 1)
	return m.pages[base][pa-base : pa-base+uint64(n)]
}

type fakeCard struct {
	mu      sync.Mutex
	reg     map[uint32]uint32
	eeprom  [3]uint16
	mem     *fakeMem
	irq     *haltest.FakeIRQ
	sent    [][]byte
	stalled bool // the transmitter does not move (a full ring)
}

func newFakeCard() *fakeCard {
	return &fakeCard{
		reg:    map[uint32]uint32{regSTATUS: statusLU},
		eeprom: [3]uint16{0x5452, 0x1200, 0x5634}, // 52:54:00:12:34:56
		mem:    newFakeMem(),
		irq:    haltest.NewFakeIRQ(),
	}
}

func (c *fakeCard) Read(off uint32) uint32 {
	c.mu.Lock()
	defer c.mu.Unlock()
	v := c.reg[off]
	if off == regICR {
		c.reg[regICR] = 0
	}
	return v
}

func (c *fakeCard) Write(off, v uint32) {
	c.mu.Lock()
	defer c.mu.Unlock()
	switch off {
	case regCTRL:
		v &^= ctrlRST // the reset is over at once
	case regEERD:
		if v&eerdStart != 0 {
			v = eerdDone | uint32(c.eeprom[(v>>8)&0xFF])<<16
		}
	case regIMS:
		c.reg[regIMS] |= v
		return
	case regIMC:
		c.reg[regIMS] &^= v
		return
	}
	c.reg[off] = v
	if off == regTDT && !c.stalled {
		c.transmit()
	}
}

func (c *fakeCard) desc(base uint32, i uint32) []byte {
	pa := uint64(c.reg[base]) | uint64(c.reg[base+4])<<32
	return c.mem.at(pa+uint64(i*descBytes), descBytes)
}

// transmit sends every descriptor from the head to the tail.
func (c *fakeCard) transmit() {
	n := c.reg[regTDLEN] / descBytes
	for c.reg[regTDH] != c.reg[regTDT] {
		d := c.desc(regTDBAL, c.reg[regTDH])
		length := int(binary.LittleEndian.Uint16(d[8:]))
		c.sent = append(c.sent, append([]byte(nil), c.mem.at(binary.LittleEndian.Uint64(d[0:]), length)...))
		setStatus(d, statusDD)
		c.reg[regTDH] = (c.reg[regTDH] + 1) % n
	}
	c.raise(intTXDW)
}

// unstall lets a stalled transmitter catch up.
func (c *fakeCard) unstall() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.stalled = false
	c.transmit()
}

// deliver receives frame into the descriptor at the head, if the driver
// has left the card one (head != tail); false is an overrun.
func (c *fakeCard) deliver(frame []byte) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	n := c.reg[regRDLEN] / descBytes
	if c.reg[regRCTL]&rctlEN == 0 || c.reg[regRDH] == c.reg[regRDT] {
		c.raise(intRXO)
		return false
	}
	d := c.desc(regRDBAL, c.reg[regRDH])
	copy(c.mem.at(binary.LittleEndian.Uint64(d[0:]), len(frame)), frame)
	binary.LittleEndian.PutUint16(d[8:], uint16(len(frame)))
	setStatus(d, statusDD|rxEOP)
	c.reg[regRDH] = (c.reg[regRDH] + 1) % n
	c.raise(intRXT0)
	return true
}

// setStatus writes a descriptor's status word last and atomically, as the
// card's write-back is what publishes the descriptor to the driver.
func setStatus(d []byte, status uint8) {
	atomic.StoreUint32((*uint32)(unsafe.Pointer(&d[descStatus])), uint32(status))
}

func (c *fakeCard) raise(cause uint32) {
	c.reg[regICR] |= cause
	if c.reg[regIMS]&cause != 0 {
		c.irq.Fire()
	}
}

func (c *fakeCard) sentFrames() [][]byte {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([][]byte(nil), c.sent...)
}
