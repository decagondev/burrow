package haltest

import "sync"

// FakeCMOS is the PC's CMOS behind ports 0x70 (index) and 0x71 (data), as
// the RTC driver sees it. Regs are the 128 registers; while Updating > 0,
// each read of status register A reports an update in progress (bit 7) and
// counts Updating down, as a clock in the middle of ticking would.
type FakeCMOS struct {
	mu       sync.Mutex
	Regs     [128]byte
	Updating int
	// Tick, if set, runs after each data read: a test moves the clock with it.
	Tick  func(regs *[128]byte)
	index byte
}

const (
	cmosIndex = 0x70
	cmosData  = 0x71
	cmosRegA  = 0x0A
	cmosUIP   = 0x80
)

func (c *FakeCMOS) In8(port uint16) uint8 {
	c.mu.Lock()
	defer c.mu.Unlock()
	if port != cmosData {
		return 0xFF
	}
	v := c.Regs[c.index]
	if c.index == cmosRegA && c.Updating > 0 {
		c.Updating--
		v |= cmosUIP
	}
	if c.Tick != nil {
		c.Tick(&c.Regs)
	}
	return v
}

// Out8 to the index port selects a register (bit 7, the NMI mask, is
// ignored); writes to the data port change the register.
func (c *FakeCMOS) Out8(port uint16, v uint8) {
	c.mu.Lock()
	defer c.mu.Unlock()
	switch port {
	case cmosIndex:
		c.index = v & 0x7F
	case cmosData:
		c.Regs[c.index] = v
	}
}
