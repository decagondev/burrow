package bga

import (
	"strings"
	"testing"

	"burrow/kernel/bootinfo"
	"burrow/kernel/driver"
	"burrow/kernel/gfx"
	"burrow/kernel/klog"
)

// dispi is the card's register file behind the index/data port pair.
type dispi struct {
	index  uint16
	regs   [10]uint16
	writes []string
}

func (d *dispi) Out16(port, v uint16) {
	switch port {
	case portIndex:
		d.index = v
	case portData:
		d.regs[d.index] = v
		d.writes = append(d.writes, string(rune('0'+d.index)))
		if d.index == regXRes {
			d.regs[regVirtWidth] = v + 32 // a card with row padding (PIT-27)
		}
	}
}

func (d *dispi) In16(port uint16) uint16 {
	if port == portData {
		return d.regs[d.index]
	}
	return 0
}

func TestSetMode(t *testing.T) {
	d := &dispi{}
	d.regs[regID] = 0xB0C5
	pitch, err := SetMode(d, 1024, 768)
	if err != nil {
		t.Fatal(err)
	}
	if pitch != (1024+32)*4 {
		t.Errorf("pitch = %d, want the card's virtual width x 4", pitch)
	}
	if got := strings.Join(d.writes, ""); got != "41234" {
		t.Errorf("register write order %s, want disable, x, y, bpp, enable (41234)", got)
	}
	if d.regs[regEnable] != enEnabled|enLinearFrame || d.regs[regBPP] != 32 || d.regs[regYRes] != 768 {
		t.Errorf("registers %v", d.regs)
	}
}

func TestSetModeRejectsNoCard(t *testing.T) {
	if _, err := SetMode(&dispi{}, 1024, 768); err == nil {
		t.Error("SetMode accepted dispi id 0")
	}
}

// W21-S03: with a firmware framebuffer the display maps it as given, on any
// adapter: no Bochs device, no DISPI writes, its pitch and its layout.
func TestStartTakesTheFirmwareMode(t *testing.T) {
	fw := bootinfo.Framebuffer{Source: bootinfo.FbSourceVBE, Phys: 0xE0000000, Width: 1280, Height: 1024,
		Pitch: 5248, BPP: 32, Red: bootinfo.Field{Size: 8, Pos: 0}, Green: bootinfo.Field{Size: 8, Pos: 8},
		Blue: bootinfo.Field{Size: 8, Pos: 16}}
	var mapped [2]uint64
	var log strings.Builder
	env := &driver.Env{Framebuffer: fw, Log: klog.New(&log, klog.Info),
		MapMMIO: func(pa, n uint64) ([]byte, error) { mapped = [2]uint64{pa, n}; return make([]byte, n), nil }}
	d := &display{}
	if !d.Probe(env) || d.bga {
		t.Fatal("Probe: want the firmware's display and no Bochs adapter")
	}
	if err := d.Start(env); err != nil {
		t.Fatal(err)
	}
	if mapped != [2]uint64{0xE0000000, 5248 * 1024} {
		t.Errorf("mapped %#x+%d", mapped[0], mapped[1])
	}
	m := Current()
	if m.Width != 1280 || m.Pitch != 5248 || m.Source != "vbe" || m.Format.Red.Pos != 0 || m.Format.Blue.Pos != 16 {
		t.Errorf("mode %+v", m)
	}
	fb := Screen()
	fb.Set(0, 1, gfx.RGB(0xFF, 0, 0)) // BGR: red is the lowest byte of row 1
	if fb.Mem[5248] != 0xFF || fb.Mem[5250] != 0 {
		t.Errorf("row 1 starts % x", fb.Mem[5248:5252])
	}
	if !strings.Contains(log.String(), "1280x1024x32 linear framebuffer at 0xe0000000 (pitch 5248, vbe)") {
		t.Errorf("log %q", log.String())
	}
}

// A layout gfx cannot draw is refused and DISPI sets its own mode instead.
func TestFirmwareModeRefusesABadLayout(t *testing.T) {
	bad := bootinfo.Framebuffer{Source: bootinfo.FbSourceVBE, BPP: 8, Red: bootinfo.Field{Size: 3, Pos: 5}}
	if _, ok, err := FirmwareMode(bad); ok || err == nil {
		t.Errorf("8 bpp: ok %v, err %v", ok, err)
	}
	if _, ok, err := FirmwareMode(bootinfo.Framebuffer{}); ok || err != nil {
		t.Errorf("none: ok %v, err %v", ok, err)
	}
}
