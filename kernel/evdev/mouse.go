package evdev

// mouse.go - /dev/input/event1 (W14-S02): mouse motion and buttons as
// Linux input events. The PS/2 driver feeds decoded packets; Feed turns
// them into EV_REL (REL_X, REL_Y, REL_WHEEL) and EV_KEY (BTN_LEFT, BTN_RIGHT,
// BTN_MIDDLE) events, one EV_SYN per packet, and hands the same packet to
// /dev/input/mice. Unlike the keyboard it grabs nothing: the console has no
// use for the mouse.

import "time"

// Linux codes (<linux/input-event-codes.h>).
const (
	RelX      = 0x00
	RelY      = 0x01
	RelWheel  = 0x08
	BtnLeft   = 0x110
	BtnRight  = 0x111
	BtnMiddle = 0x112
)

// Button bits in Feed's buttons, as in a PS/2 packet's first byte.
const (
	ButtonLeft   = 1 << 0
	ButtonRight  = 1 << 1
	ButtonMiddle = 1 << 2
)

var buttonCodes = [3]uint16{BtnLeft, BtnRight, BtnMiddle}

// Mouse is the mouse's event device.
type Mouse struct {
	core
	buttons uint8
	mice    *Mice
}

// NewMouse makes the mouse; packets also go to mice (nil: nowhere else).
func NewMouse(clock func() time.Duration, mice *Mice) *Mouse {
	id := identity{name: mouseName, phys: "isa0060/serio1/input0", product: 2,
		keys: buttonCodes[:], rels: []uint16{RelX, RelY, RelWheel}}
	return &Mouse{core: newCore(id, clock), mice: mice}
}

// Feed takes one packet in screen directions: dx > 0 is right, dy > 0 is
// down, wheel > 0 is away from the user (up); buttons holds the Button bits.
func (m *Mouse) Feed(dx, dy, wheel int, buttons uint8) {
	m.mu.Lock()
	t := m.clock()
	var ev []byte
	for _, r := range []struct {
		code uint16
		v    int
	}{{RelX, dx}, {RelY, dy}, {RelWheel, wheel}} {
		if r.v != 0 {
			ev = append(ev, event(t, EvRel, r.code, int32(r.v))...)
		}
	}
	for i, code := range buttonCodes {
		bit := uint8(1) << i
		if (buttons^m.buttons)&bit == 0 {
			continue
		}
		v := int32(0)
		if buttons&bit != 0 {
			v = 1
			m.down[code] = true
		} else {
			delete(m.down, code)
		}
		ev = append(ev, event(t, EvKey, code, v)...)
	}
	m.buttons = buttons
	if len(ev) > 0 {
		m.send(append(ev, event(t, EvSyn, SynReport, 0)...))
	}
	m.mu.Unlock()
	if m.mice != nil {
		m.mice.feed(dx, dy, wheel, buttons)
	}
}
