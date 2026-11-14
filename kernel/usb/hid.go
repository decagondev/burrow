// hid.go turns HID boot-protocol reports into what the PS/2 path already
// carries (W27-S03): a keyboard's 8-byte report into scancode set 1 bytes
// (so the console's decoder, /dev/input/event0 and the VT keys need no
// second keyboard path), a mouse's report into a relative move. Pure: it
// keeps only the previous report. A USB keyboard does not repeat keys by
// itself, and neither does this. HID 1.11 appendix B; HID Usage Tables
// §10 (Keyboard/Keypad page); scancodes as drivers/ps2's decoder reads them.
package usb

const e0 = 0x100 // the code is sent after an 0xE0 prefix

// set1 maps a keyboard usage (0x04-0x67) to its set 1 make code.
var set1 = [0x68]uint16{
	0x04: 0x1E, 0x30, 0x2E, 0x20, 0x12, 0x21, 0x22, 0x23, 0x17, 0x24, 0x25, 0x26, 0x32, 0x31, // a-n
	0x12: 0x18, 0x19, 0x10, 0x13, 0x1F, 0x14, 0x16, 0x2F, 0x11, 0x2D, 0x15, 0x2C, // o-z
	0x1E: 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0A, 0x0B, // 1-0
	0x28: 0x1C, 0x01, 0x0E, 0x0F, 0x39, 0x0C, 0x0D, 0x1A, 0x1B, 0x2B, 0x2B, 0x27, 0x28, 0x29, 0x33, 0x34, 0x35, 0x3A,
	0x3A: 0x3B, 0x3C, 0x3D, 0x3E, 0x3F, 0x40, 0x41, 0x42, 0x43, 0x44, 0x57, 0x58, // F1-F12
	0x46: e0 | 0x37, 0x46, 0, // PrintScreen (SysRq with Alt: see Feed), ScrollLock, Pause
	0x49: e0 | 0x52, e0 | 0x47, e0 | 0x49, e0 | 0x53, e0 | 0x4F, e0 | 0x51, // Insert Home PgUp Delete End PgDn
	0x4F: e0 | 0x4D, e0 | 0x4B, e0 | 0x50, e0 | 0x48, // Right Left Down Up
	0x53: 0x45, e0 | 0x35, 0x37, 0x4A, 0x4E, e0 | 0x1C, // NumLock, keypad / * - + Enter
	0x59: 0x4F, 0x50, 0x51, 0x4B, 0x4C, 0x4D, 0x47, 0x48, 0x49, 0x52, 0x53, // keypad 1-9 0 .
	0x64: 0x56, e0 | 0x5D, // the key beside left shift, Menu
}

// modifiers maps the report's modifier bits 0-7 (LCtrl LShift LAlt LGUI
// RCtrl RShift RAlt RGUI) to their make codes.
var modifiers = [8]uint16{0x1D, 0x2A, 0x38, e0 | 0x5B, e0 | 0x1D, 0x36, e0 | 0x38, e0 | 0x5C}

const (
	usagePrintScreen = 0x46
	usageRollOver    = 0x01 // the report says too many keys are down
	scSysRq          = 0x54 // Alt+PrintScreen's set 1 code (cmd/kernel/vtkeys.go)
	modAlt           = 1<<2 | 1<<6
	breakBit         = 0x80
)

// Keyboard keeps the last boot report.
type Keyboard struct {
	mods uint8
	keys [6]byte
}

// Feed takes one boot report (modifiers, reserved, six usages) and returns
// the set 1 bytes for what changed: releases first, then presses.
func (k *Keyboard) Feed(r []byte) []byte {
	if len(r) < 8 {
		return nil
	}
	var keys [6]byte
	copy(keys[:], r[2:8])
	for _, u := range keys {
		if u == usageRollOver {
			return nil // a phantom state: keep the last one
		}
	}
	mods := r[0]
	var out []byte
	for _, u := range k.keys {
		if u != 0 && !has(keys, u) {
			out = k.code(out, u, true, k.mods)
		}
	}
	for i := range 8 {
		was, is := k.mods&(1<<i) != 0, mods&(1<<i) != 0
		if was != is {
			out = emit(out, modifiers[i], was)
		}
	}
	for _, u := range keys {
		if u != 0 && !has(k.keys, u) {
			out = k.code(out, u, false, mods)
		}
	}
	k.mods, k.keys = mods, keys
	return out
}

func (k *Keyboard) code(out []byte, u byte, release bool, mods uint8) []byte {
	if int(u) >= len(set1) || set1[u] == 0 {
		return out
	}
	c := set1[u]
	if u == usagePrintScreen && mods&modAlt != 0 {
		c = scSysRq
	}
	return emit(out, c, release)
}

func emit(out []byte, c uint16, release bool) []byte {
	if c&e0 != 0 {
		out = append(out, 0xE0)
	}
	b := byte(c)
	if release {
		b |= breakBit
	}
	return append(out, b)
}

func has(keys [6]byte, u byte) bool {
	for _, k := range keys {
		if k == u {
			return true
		}
	}
	return false
}

// MouseReport reads a boot mouse report: buttons (bit 0 left, 1 right, 2
// middle), then dx and dy (dy > 0 down), then a wheel byte if the device
// sends one (> 0 up), as driver.Env.Mouse wants them.
func MouseReport(r []byte) (dx, dy, wheel int, buttons uint8, ok bool) {
	if len(r) < 3 {
		return 0, 0, 0, 0, false
	}
	if len(r) > 3 {
		wheel = int(int8(r[3]))
	}
	return int(int8(r[1])), int(int8(r[2])), wheel, r[0] & 7, true
}
