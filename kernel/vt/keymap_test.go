package vt

import "testing"

func TestKeymapEntry(t *testing.T) {
	for _, c := range []struct {
		table, index uint8
		want         uint16
	}{
		{tablePlain, 30, 0x0b61},       // a: K(KT_LETTER,'a')
		{tableShift, 30, 0x0b41},       // A
		{tablePlain, 2, 0x0031},        // 1: K(KT_LATIN,'1')
		{tableShift, 2, 0x0021},        // !
		{tablePlain, 28, 0x0201},       // Enter: K_ENTER
		{tablePlain, 42, 0x0700},       // left Shift: K_SHIFT
		{tablePlain, 29, 0x0702},       // left Ctrl: K_CTRL
		{tablePlain, 56, 0x0703},       // left Alt: K_ALT
		{tablePlain, 59, 0x0100},       // F1: K(KT_FN,0)
		{tablePlain, 103, 0x0603},      // Up: K_UP
		{tableAltGr, 30, 0x0200},       // AltGr+a: K_HOLE
		{tableAltGrShift, 42, 0x0700},  // modifiers in every table
		{tablePlain, 0, 0x0200},        // key code 0: nothing
		{tablePlain, 57, 0x0020},       // space
		{tableShift, 40, uint16('"')},  // Shift+'
		{tablePlain, 14, uint16(0x7f)}, // Backspace sends DEL
	} {
		if got, ok := KeymapEntry(c.table, c.index); !ok || got != c.want {
			t.Errorf("table %d key %d = %#04x, want %#04x", c.table, c.index, got, c.want)
		}
	}
	if _, ok := KeymapEntry(0, 200); ok {
		t.Error("key code 200 accepted")
	}
}
