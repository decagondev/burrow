package vt

import (
	"bytes"
	"testing"
)

func TestKeyModes(t *testing.T) {
	v := New()
	if c, raw := v.Key(0x1E); !c || raw != nil {
		t.Error("K_XLATE: the console translates")
	}
	v.SetKbMode(7, KRaw)
	if c, raw := v.Key(0x9E); c || !bytes.Equal(raw, []byte{0x9E}) {
		t.Errorf("K_RAW: %v % x", c, raw)
	}
	v.SetKbMode(7, KMediumRaw)
	if _, raw := v.Key(0x1E); !bytes.Equal(raw, []byte{30}) { // KEY_A
		t.Errorf("K_MEDIUMRAW press A = % x", raw)
	}
	v.Key(0xE0)
	if _, raw := v.Key(0xC8); !bytes.Equal(raw, []byte{103 | 0x80}) { // KEY_UP released
		t.Errorf("K_MEDIUMRAW release up = % x", raw)
	}
	v.SetKbMode(7, KOff)
	if c, raw := v.Key(0x1E); c || raw != nil {
		t.Error("K_OFF: keys go nowhere")
	}
	if err := v.SetKbMode(7, 9); err == nil {
		t.Error("mode 9 accepted")
	}
}

// What a process set is undone when it exits; what another set stays.
func TestRelease(t *testing.T) {
	v := New()
	var screen []bool
	v.Graphics = func(g bool) { screen = append(screen, g) }
	v.SetKDMode(5, KDGraphics)
	v.SetKbMode(5, KOff)
	v.SetVTMode(6, Mode{Mode: VTProcess, Relsig: 10})
	v.Release(6)
	if v.VTMode().Mode != VTAuto || v.KbMode() != KOff || v.KDMode() != KDGraphics {
		t.Error("Release(6) touched what 5 set, or kept what 6 set")
	}
	v.Release(5)
	if v.KbMode() != KXlate || v.KDMode() != KDText {
		t.Errorf("after the exit: keyboard %d display %d", v.KbMode(), v.KDMode())
	}
	if len(screen) != 2 || !screen[0] || screen[1] {
		t.Errorf("Graphics calls %v, want [true false]", screen)
	}
	v.SetKDMode(5, KDText) // no change: no call
	if len(screen) != 2 {
		t.Error("a mode set to what it was called Graphics")
	}
}
