package evdev

// tablet.go - /dev/input/event2 (W22-S05, E18.F06): an absolute pointer,
// QEMU's virtio tablet. The virtioinput driver hands on the device's own
// events (EV_ABS ABS_X/ABS_Y, EV_KEY buttons, EV_SYN); they are queued per
// packet, at SYN_REPORT, with EVIOCGABS telling programs the axes' ranges.
// Readers of /dev/input/mice, a relative stream, get the same packets as
// moves: the position scaled to the screen's current size, minus where the
// last packet put it. Never blocks the driver.

import (
	"encoding/binary"
	"time"
)

const (
	EvAbs = 3
	AbsX  = 0x00
	AbsY  = 0x01

	busVirtual = 0x06
	qemuVendor = 0x0627
)

// absAxis is an absolute axis a device reports, with its range.
type absAxis struct {
	code     uint16
	min, max int32
}

// Tablet is an absolute pointer's event device.
type Tablet struct {
	core
	mice    *Mice
	screen  func() (w, h int)
	pending []byte
	buttons uint8
	// where mice readers were last told the pointer is (pixels), and the
	// mice session that was told
	px, py  int
	session uint64
	moved   [2]bool
}

// NewTablet makes the tablet: name and ranges as the device reports them;
// screen gives the current mode, for the moves mice readers get (nil mice:
// none).
func NewTablet(clock func() time.Duration, name string, x, y [2]int32, mice *Mice, screen func() (w, h int)) *Tablet {
	id := identity{name: name, phys: "virtio0/input0", bus: busVirtual, vendor: qemuVendor, product: 1,
		keys: buttonCodes[:], abs: []absAxis{{AbsX, x[0], x[1]}, {AbsY, y[0], y[1]}}}
	return &Tablet{core: newCore(id, clock), mice: mice, screen: screen}
}

// Feed takes one event from the device; a SYN_REPORT ends a packet.
func (t *Tablet) Feed(typ, code uint16, value int32) {
	t.mu.Lock()
	now := t.clock()
	t.pending = append(t.pending, event(now, typ, code, value)...)
	switch typ {
	case EvAbs:
		if code == AbsX || code == AbsY {
			t.absValue[code] = value
			t.moved[code] = true
		}
	case EvKey:
		for i, c := range buttonCodes {
			if c != code {
				continue
			}
			if value != 0 {
				t.buttons |= 1 << i
				t.down[code] = true
			} else {
				t.buttons &^= 1 << i
				delete(t.down, code)
			}
		}
	}
	if typ != EvSyn || code != SynReport {
		t.mu.Unlock()
		return
	}
	t.send(t.pending)
	t.pending = nil
	dx, dy, buttons, ok := t.move()
	t.mu.Unlock()
	if ok {
		t.mice.feed(dx, dy, 0, buttons)
	}
}

// move is the packet as a relative move for mice readers (t.mu held). A
// new mice session (its first open) starts at the screen's centre, where
// pointer-tracking readers start theirs. At an edge the move overshoots by
// a screen, so a reader that missed packets (TinyX's protocol probe eats
// the first few) clamps to the same edge and is in step again.
func (t *Tablet) move() (dx, dy int, buttons uint8, ok bool) {
	if t.mice == nil || t.screen == nil {
		return 0, 0, 0, false
	}
	w, h := t.screen()
	if s := t.mice.session(); s != t.session {
		t.session, t.px, t.py = s, w/2, h/2
	}
	axis := func(code uint16, size, at int) (int, int) {
		a := t.id.abs[code]
		p := scale(t.absValue[code], a.min, a.max, size)
		d := p - at
		if t.moved[code] && p == 0 {
			d -= size
		} else if t.moved[code] && p == size-1 {
			d += size
		}
		return p, d
	}
	t.px, dx = axis(AbsX, w, t.px)
	t.py, dy = axis(AbsY, h, t.py)
	t.moved = [2]bool{}
	return dx, dy, t.buttons, true
}

// scale maps v in [min, max] onto [0, size): equal shares of the range.
func scale(v, lo, hi int32, size int) int {
	if hi <= lo || size <= 0 {
		return 0
	}
	v = max(lo, min(hi, v))
	return int(int64(v-lo) * int64(size) / (int64(hi-lo) + 1))
}

// absinfo is struct input_absinfo for EVIOCGABS: value, minimum, maximum,
// fuzz, flat, resolution (s32 each).
func (c *core) absinfo(code uint16) ([]byte, bool) {
	for _, a := range c.id.abs {
		if a.code != code {
			continue
		}
		c.mu.Lock()
		v := c.absValue[code]
		c.mu.Unlock()
		b := make([]byte, 24)
		le := binary.LittleEndian
		le.PutUint32(b, uint32(v))
		le.PutUint32(b[4:], uint32(a.min))
		le.PutUint32(b[8:], uint32(a.max))
		return b, true
	}
	return nil, false
}
