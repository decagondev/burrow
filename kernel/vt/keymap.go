package vt

// keymap.go - the console's keymap as KDGKBENT reports it (W16-S02): a US
// layout in Linux's K(type, value) form, for the plain, Shift, AltGr and
// AltGr+Shift tables that X servers on the console read (TinyX builds its
// whole X keymap from them). Key codes are Linux's (evdev's). Never
// changes: KDSKBENT is accepted and ignored. Reference: <linux/keyboard.h>,
// Linux drivers/tty/vt/defkeymap.map.

// Linux key types and the special values among them.
const (
	ktLatin  = 0
	ktFn     = 1
	ktSpec   = 2
	ktPad    = 3
	ktCur    = 6
	ktShift  = 7
	ktLetter = 11

	kHole  = ktSpec<<8 | 0 // K_HOLE: nothing on this key in this table
	kEnter = ktSpec<<8 | 1
	kCaps  = ktSpec<<8 | 7
	kNum   = ktSpec<<8 | 8
	kHold  = ktSpec<<8 | 9 // Scroll Lock

	kgShift, kgAltGr, kgCtrl, kgAlt = 0, 1, 2, 3

	// The tables KDGKBENT is asked for: modifier bits (1<<KG_*).
	tablePlain       = 0
	tableShift       = 1 << kgShift
	tableAltGr       = 1 << kgAltGr
	tableAltGrShift  = 1<<kgAltGr | 1<<kgShift
	keymapKeys       = 128 // NR_KEYS
	firstLetterTable = tableShift
)

func k(typ, val int) uint16 { return uint16(typ<<8 | val) }

// printable are the keys with a character, plain and with Shift (key code:
// "ab": a plain, b shifted). Letters are KT_LETTER (Caps Lock acts on them).
var printable = map[uint8]string{
	2: "1!", 3: "2@", 4: "3#", 5: "4$", 6: "5%", 7: "6^", 8: "7&", 9: "8*", 10: "9(", 11: "0)",
	12: "-_", 13: "=+", 26: "[{", 27: "]}", 39: ";:", 40: "'\"", 41: "`~", 43: "\\|",
	51: ",<", 52: ".>", 53: "/?", 57: "  ",
	16: "qQ", 17: "wW", 18: "eE", 19: "rR", 20: "tT", 21: "yY", 22: "uU", 23: "iI", 24: "oO", 25: "pP",
	30: "aA", 31: "sS", 32: "dD", 33: "fF", 34: "gG", 35: "hH", 36: "jJ", 37: "kK", 38: "lL",
	44: "zZ", 45: "xX", 46: "cC", 47: "vV", 48: "bB", 49: "nN", 50: "mM",
}

// same are the keys that mean the same in every table: modifiers, editing
// and cursor keys, the function keys, the keypad.
var same = map[uint8]uint16{
	1: k(ktLatin, 27), 14: k(ktLatin, 127), 15: k(ktLatin, 9), 28: kEnter,
	29: k(ktShift, kgCtrl), 97: k(ktShift, kgCtrl), 42: k(ktShift, kgShift), 54: k(ktShift, kgShift),
	56: k(ktShift, kgAlt), 100: k(ktShift, kgAlt), 58: kCaps, 69: kNum, 70: kHold,
	87: k(ktFn, 10), 88: k(ktFn, 11), // F11, F12
	71: k(ktPad, 7), 72: k(ktPad, 8), 73: k(ktPad, 9), 74: k(ktPad, 11), // KP7-9, KP-
	75: k(ktPad, 4), 76: k(ktPad, 5), 77: k(ktPad, 6), 78: k(ktPad, 10), // KP4-6, KP+
	79: k(ktPad, 1), 80: k(ktPad, 2), 81: k(ktPad, 3), 82: k(ktPad, 0), 83: k(ktPad, 16), // KP1-3, KP0, KP.
	55: k(ktPad, 12), 98: k(ktPad, 13), 96: k(ktPad, 14), // KP*, KP/, KP Enter
	102: k(ktFn, 20), 110: k(ktFn, 21), 111: k(ktFn, 22), 107: k(ktFn, 23), // Home, Insert, Delete, End
	104: k(ktFn, 24), 109: k(ktFn, 25), // Page Up, Page Down
	103: k(ktCur, 3), 105: k(ktCur, 1), 106: k(ktCur, 2), 108: k(ktCur, 0), // Up, Left, Right, Down
}

func init() {
	for i := 0; i < 10; i++ { // F1-F10 are key codes 59-68
		same[uint8(59+i)] = k(ktFn, i)
	}
}

// KeymapEntry is KDGKBENT: what key code index means in table, or K_HOLE.
func KeymapEntry(table, index uint8) (uint16, bool) {
	if index >= keymapKeys {
		return 0, false
	}
	if v, ok := same[index]; ok {
		return v, true
	}
	s, ok := printable[index]
	if !ok || table != tablePlain && table != tableShift {
		return kHole, true // AltGr adds nothing on a US layout
	}
	ch := int(s[0])
	if table == tableShift {
		ch = int(s[1])
	}
	if s[0] >= 'a' && s[0] <= 'z' {
		return k(ktLetter, ch), true
	}
	return k(ktLatin, ch), true
}
