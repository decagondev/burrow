package evdev

// ioctl.go - the event-device ioctls programs use to find out what a device
// is before they read it (W11-S02, W14-S02): version, id, name, physical
// path, EVIOCGRAB, and the bitmaps EVIOCGBIT (event types, then the codes of
// one type), EVIOCGKEY (keys held now), EVIOCGPROP, EVIOCGLED and EVIOCGSW
// (none here). Toolkits (Qt, libinput, X servers) classify devices by
// EVIOCGBIT alone: keys and no relative axes is a keyboard, REL_X/REL_Y with
// BTN_LEFT is a mouse. Requests are _IOC(dir, 'E', nr, size).

import (
	"encoding/binary"
	"syscall"
)

const (
	eviocgversion = 0x80044501
	eviocgid      = 0x80084502
	eviocgrab     = 0x40044590
	iocMaskNoSize = 0xC000FFFF // direction, type and number without the size
	iocRead       = 0x80000000

	nrName = 0x06
	nrPhys = 0x07
	nrUniq = 0x08
	nrProp = 0x09
	nrKey  = 0x18
	nrLED  = 0x19
	nrSnd  = 0x1A
	nrSw   = 0x1B
	nrBit  = 0x20 // + event type, up to EV_MAX (0x1F)
	evMax  = 0x1F

	version      = 0x010001
	busI8042     = 0x11
	keyboardName = "Burrow PS/2 keyboard"
	mouseName    = "Burrow PS/2 mouse"
)

// bitmap sets the given bits in a bitmap of n bytes.
func bitmap(n int, bits ...uint16) []byte {
	b := make([]byte, n)
	for _, bit := range bits {
		if int(bit)/8 < n {
			b[bit/8] |= 1 << (bit % 8)
		}
	}
	return b
}

// bits is the EVIOCGBIT answer for event type ev (0: the types themselves).
func (c *core) bits(ev uint16, n int) []byte {
	switch ev {
	case 0:
		types := []uint16{EvSyn, EvKey}
		if len(c.id.rels) > 0 {
			types = append(types, EvRel)
		}
		return bitmap(n, types...)
	case EvKey:
		return bitmap(n, c.id.keys...)
	case EvRel:
		return bitmap(n, c.id.rels...)
	}
	return bitmap(n)
}

func (h *handle) Ioctl(req, arg uint64, in, out func(va uint64, b []byte) error) (int64, error) {
	c := h.d
	switch req {
	case eviocgversion:
		return 0, out(arg, binary.LittleEndian.AppendUint32(nil, version))
	case eviocgid: // bustype, vendor, product, version
		return 0, out(arg, []byte{busI8042, 0, 1, 0, byte(c.id.product), 0, 1, 0})
	case eviocgrab:
		return 0, nil // always granted: an open keyboard already has the keys
	}
	if req&iocMaskNoSize&^0xFF != iocRead|'E'<<8 {
		return 0, syscall.ENOTTY
	}
	size, nr := int(req>>16&0x3FFF), req&0xFF
	reply := func(b []byte) (int64, error) {
		if len(b) > size {
			b = b[:size]
		}
		return int64(len(b)), out(arg, b)
	}
	switch {
	case nr == nrName:
		return reply(append([]byte(c.id.name), 0))
	case nr == nrPhys:
		return reply(append([]byte(c.id.phys), 0))
	case nr == nrUniq:
		return 0, syscall.ENOENT // no unique id, as on Linux
	case nr == nrKey:
		c.mu.Lock()
		down := make([]uint16, 0, len(c.down))
		for k := range c.down {
			down = append(down, k)
		}
		c.mu.Unlock()
		return reply(bitmap(size, down...))
	case nr == nrProp || nr == nrLED || nr == nrSnd || nr == nrSw:
		return reply(bitmap(size))
	case nr >= nrBit && nr <= nrBit+evMax:
		return reply(c.bits(uint16(nr-nrBit), size))
	}
	return 0, syscall.ENOTTY
}
