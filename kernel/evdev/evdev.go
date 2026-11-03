// Package evdev is /dev/input/event0 (W11-S02, E10.F02): the keyboard as
// Linux input events. It takes raw scancode set 1 bytes from the PS/2
// driver, turns them into Linux key codes, and gives every open file its
// own queue of struct input_event (EV_KEY press 1 / release 0 / repeat 2,
// each followed by EV_SYN). While any file has it open the keyboard is
// grabbed: Feed reports that, and the driver then keeps the keys from the
// text console. Never blocks the driver: a full queue drops events.
// Layouts: <linux/input.h>. Docs: docs/04-plan/waves/WAVE-11-doom.md.
package evdev

import (
	"encoding/binary"
	"sync"
	"syscall"
	"time"

	"burrow/kernel/fs/devfs"
)

const (
	EventSize = 24 // struct input_event: timeval (16), type, code (u16), value (s32)

	EvSyn     = 0
	EvKey     = 1
	SynReport = 0

	queueEvents = 256 // per open file; Linux's evdev buffer is of this order

	prefixE0  = 0xE0
	releaseFl = 0x80
)

// Linux key codes for keys after an 0xE0 prefix (<linux/input-event-codes.h>).
// Without the prefix, set 1 make codes 0x01-0x58 are the Linux codes.
var extended = map[byte]uint16{
	0x1C: 96, 0x1D: 97, 0x35: 98, 0x38: 100, // KPENTER RIGHTCTRL KPSLASH RIGHTALT
	0x47: 102, 0x48: 103, 0x49: 104, 0x4B: 105, 0x4D: 106, // HOME UP PAGEUP LEFT RIGHT
	0x4F: 107, 0x50: 108, 0x51: 109, 0x52: 110, 0x53: 111, // END DOWN PAGEDOWN INSERT DELETE
	0x5B: 125, 0x5C: 126, 0x5D: 127, // LEFTMETA RIGHTMETA COMPOSE
}

const maxPlain = 0x58 // KEY_F12

// KeyCode is the Linux key code of a set 1 make code (0 if there is none;
// e0 says the 0xE0 prefix came first).
func KeyCode(e0 bool, code byte) uint16 {
	if e0 {
		return extended[code]
	}
	if code == 0 || code > maxPlain {
		return 0
	}
	return uint16(code)
}

// Device is the keyboard's event device.
type Device struct {
	clock func() time.Duration // event timestamps

	mu    sync.Mutex
	e0    bool
	down  map[uint16]bool
	opens map[*handle]bool
}

// New makes the device; clock gives the time stamped on events.
func New(clock func() time.Duration) *Device {
	return &Device{clock: clock, down: map[uint16]bool{}, opens: map[*handle]bool{}}
}

// Feed takes one scancode byte from the keyboard and queues the event it
// makes for every open file. grabbed: some file has the device open, so the
// key is the program's and not the console's.
func (d *Device) Feed(sc byte) (grabbed bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	grabbed = len(d.opens) > 0
	if sc == prefixE0 {
		d.e0 = true
		return grabbed
	}
	e0 := d.e0
	d.e0 = false
	code := KeyCode(e0, sc&^releaseFl)
	if code == 0 {
		return grabbed // unknown, or the fake shifts around print screen
	}
	value := int32(1)
	switch {
	case sc&releaseFl != 0:
		value = 0
		delete(d.down, code)
	case d.down[code]:
		value = 2 // typematic repeat
	default:
		d.down[code] = true
	}
	t := d.clock()
	ev := append(event(t, EvKey, code, value), event(t, EvSyn, SynReport, 0)...)
	for h := range d.opens {
		h.push(ev)
	}
	return grabbed
}

// Grabbed reports whether a program holds the device.
func (d *Device) Grabbed() bool { d.mu.Lock(); defer d.mu.Unlock(); return len(d.opens) > 0 }

func event(t time.Duration, typ, code uint16, value int32) []byte {
	b := make([]byte, EventSize)
	le := binary.LittleEndian
	le.PutUint64(b[0:], uint64(t/time.Second))
	le.PutUint64(b[8:], uint64(t%time.Second/time.Microsecond))
	le.PutUint16(b[16:], typ)
	le.PutUint16(b[18:], code)
	le.PutUint32(b[20:], uint32(value))
	return b
}

// Open gives each open file its own queue (devfs.Opener).
func (d *Device) Open() (devfs.Device, error) {
	h := &handle{d: d, changed: make(chan struct{})}
	d.mu.Lock()
	d.opens[h] = true
	d.mu.Unlock()
	return h, nil
}

// The registered device itself (never opened through devfs) has no events.
func (d *Device) Read([]byte) (int, error)  { return 0, syscall.EAGAIN }
func (d *Device) Write([]byte) (int, error) { return 0, syscall.EINVAL }
