package driver

import (
	"strings"
	"testing"

	"burrow/kernel/hal"
	"burrow/kernel/haltest"
)

// The nucleus's encoding (nucleus/irq.h): kind in bits 7:0, level bit 8,
// active-low bit 9, the GSI or vector from bit 16.
func TestParseRoute(t *testing.T) {
	for _, c := range []struct {
		v    uint64
		want string
	}{
		{1 | 4<<16, "8259 line 4"},
		{2 | 2<<16, "i/o apic gsi 2 edge high"},
		{2 | 1<<8 | 1<<9 | 11<<16, "i/o apic gsi 11 level low"},
		{3 | 0x50<<16, "msi vector 0x50"},
		{0, "not routed"},
	} {
		if got := ParseRoute(c.v).String(); got != c.want {
			t.Errorf("ParseRoute(%#x) = %q, want %q", c.v, got, c.want)
		}
	}
}

// Report names each line's route, counts what it served and lists its drivers.
func TestLinesReport(t *testing.T) {
	line := haltest.NewFakeIRQ()
	defer line.Close()
	ls := NewLines(func(int) hal.IRQLine { return line }, nil)
	a := &dev{line: line}
	ls.Serve(16, "virtioblk", a.handle)
	a.raise()
	waitUntil(t, "the interrupt", func() bool { return a.served.Load() == 1 })
	route := func(n int) (Route, bool) { return Route{Kind: RouteMSI, Where: 0x50}, n == 16 }
	got := ls.Report(route)
	if want := "irq 16 (msi vector 0x50): 1 interrupts: virtioblk\n"; got != want {
		t.Errorf("Report = %q, want %q", got, want)
	}
	if got := ls.Report(nil); !strings.HasPrefix(got, "irq 16: 1 interrupts") {
		t.Errorf("Report(nil) = %q", got)
	}
}
