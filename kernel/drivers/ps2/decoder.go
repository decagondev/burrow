package ps2

// Decoder turns scancode set 1 bytes (what the 8042 delivers with
// translation on) into the bytes a terminal would send: ASCII, control codes
// for ctrl+letter, ANSI escapes for the arrow and editing keys. It is pure
// and keeps only modifier state, so it is tested byte by byte on the host.
type Decoder struct {
	lshift, rshift, lctrl, rctrl, caps bool
	e0                                 bool // the previous byte was the 0xE0 prefix
}

const (
	prefixE0  = 0xE0
	releaseFl = 0x80

	scLCtrl  = 0x1D
	scLShift = 0x2A
	scRShift = 0x36
	scCaps   = 0x3A
)

// Make codes 0x00-0x39 (US layout); "" = no character (modifiers, unknown).
var plain = [0x3A]string{
	0x01: "\x1b", 0x02: "1", "2", "3", "4", "5", "6", "7", "8", "9", "0", "-", "=", "\x7f", "\t",
	0x10: "q", "w", "e", "r", "t", "y", "u", "i", "o", "p", "[", "]", "\r",
	0x1E: "a", "s", "d", "f", "g", "h", "j", "k", "l", ";", "'", "`",
	0x2B: "\\", "z", "x", "c", "v", "b", "n", "m", ",", ".", "/",
	0x37: "*", 0x39: " ",
}

var shifted = [0x3A]string{
	0x01: "\x1b", 0x02: "!", "@", "#", "$", "%", "^", "&", "*", "(", ")", "_", "+", "\x7f", "\t",
	0x10: "Q", "W", "E", "R", "T", "Y", "U", "I", "O", "P", "{", "}", "\r",
	0x1E: "A", "S", "D", "F", "G", "H", "J", "K", "L", ":", "\"", "~",
	0x2B: "|", "Z", "X", "C", "V", "B", "N", "M", "<", ">", "?",
	0x37: "*", 0x39: " ",
}

// Keys after an 0xE0 prefix.
var extended = map[byte]string{
	0x1C: "\r", 0x35: "/",
	0x48: "\x1b[A", 0x50: "\x1b[B", 0x4D: "\x1b[C", 0x4B: "\x1b[D",
	0x47: "\x1b[H", 0x4F: "\x1b[F", 0x53: "\x1b[3~",
}

// Feed takes one byte from the keyboard and returns what it types (often
// nothing: releases, modifiers, prefixes).
func (d *Decoder) Feed(sc byte) []byte {
	if sc == prefixE0 {
		d.e0 = true
		return nil
	}
	ext := d.e0
	d.e0 = false
	release := sc&releaseFl != 0
	code := sc &^ releaseFl

	switch {
	case code == scLCtrl && ext:
		d.rctrl = !release
		return nil
	case code == scLCtrl:
		d.lctrl = !release
		return nil
	case ext && (code == scLShift || code == scRShift):
		return nil // fake shifts around print screen and friends
	case code == scLShift:
		d.lshift = !release
		return nil
	case code == scRShift:
		d.rshift = !release
		return nil
	case code == scCaps:
		if !release {
			d.caps = !d.caps
		}
		return nil
	case release:
		return nil
	case ext:
		return []byte(extended[code])
	case int(code) >= len(plain):
		return nil
	}

	s := plain[code]
	isLetter := len(s) == 1 && s[0] >= 'a' && s[0] <= 'z'
	shift := d.lshift || d.rshift
	if isLetter && d.caps {
		shift = !shift
	}
	if shift {
		s = shifted[code]
	}
	if (d.lctrl || d.rctrl) && isLetter {
		return []byte{s[0] & 0x1F} // ^A = 1 ... ^Z = 26
	}
	return []byte(s)
}
