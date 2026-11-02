package bga

import (
	"strings"
	"testing"
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
