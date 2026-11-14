package pci

import "testing"

// rwBus is fakeBus with writes: Out32 to 0xCFC stores the word.
type rwBus struct{ fakeBus }

func (b *rwBus) Out32(port uint16, v uint32) {
	if port == portData {
		b.words[b.addr] = v
		return
	}
	b.fakeBus.Out32(port, v)
}

// A function with a status that announces a list: MSI (64-bit) at 0x50,
// a vendor capability at 0x60, MSI-X (table of 4 in BAR 1 at 0x2000) at 0x70.
func withCaps() (*rwBus, Device) {
	b := &rwBus{fakeBus{words: map[uint32]uint32{}}}
	b.add(0, 4, 0, 0x1af4, 0x1042, 0x01000000, false, 11)
	w := b.words
	w[Address(0, 4, 0, offCommand)] = statusCapList | 0x0007
	w[Address(0, 4, 0, offCapPtr)] = 0x50
	w[Address(0, 4, 0, 0x50)] = 0x0080<<16 | 0x60<<8 | capMSI // 64-bit capable
	w[Address(0, 4, 0, 0x60)] = 0x70<<8 | 0x09
	w[Address(0, 4, 0, 0x70)] = 0x0003<<16 | 0x00<<8 | capMSIX
	w[Address(0, 4, 0, 0x74)] = 0x2000 | 1
	return b, Device{Bus: 0, Dev: 4, Fn: 0}
}

func TestCapabilities(t *testing.T) {
	b, d := withCaps()
	caps := Capabilities(PortConfig{IO: b}, d)
	want := []Cap{{capMSI, 0x50}, {0x09, 0x60}, {capMSIX, 0x70}}
	if len(caps) != len(want) {
		t.Fatalf("caps = %v, want %v", caps, want)
	}
	for i := range want {
		if caps[i] != want[i] {
			t.Errorf("cap %d = %v, want %v", i, caps[i], want[i])
		}
	}
	if off, ok := FindCap(caps, capMSIX); !ok || off != 0x70 {
		t.Errorf("FindCap(MSI-X) = %#x, %v", off, ok)
	}
	b.words[Address(0, 4, 0, offCommand)] = 0x0007 // no list announced
	if caps := Capabilities(PortConfig{IO: b}, d); caps != nil {
		t.Errorf("no list: caps = %v", caps)
	}
	b.words[Address(0, 4, 0, offCommand)] = statusCapList
	b.words[Address(0, 4, 0, 0x70)] = 0x50<<8 | capMSIX // a loop back to 0x50
	if caps := Capabilities(PortConfig{IO: b}, d); len(caps) != maxCapHops {
		t.Errorf("a looping list walked %d hops, want %d", len(caps), maxCapHops)
	}
}

func TestEnableMSI64(t *testing.T) {
	b, d := withCaps()
	cfg := PortConfig{IO: b}
	EnableMSI(cfg, d, 0x50, 0xFEE01000, 0x51)
	w := b.words
	if got := w[Address(0, 4, 0, 0x54)]; got != 0xFEE01000 {
		t.Errorf("address = %#x", got)
	}
	if got := w[Address(0, 4, 0, 0x58)]; got != 0 {
		t.Errorf("upper address = %#x", got)
	}
	if got := w[Address(0, 4, 0, 0x5C)]; got != 0x51 {
		t.Errorf("data = %#x", got)
	}
	if got := w[Address(0, 4, 0, 0x50)]; got&msiEnable == 0 || got&msiMultiMask != 0 {
		t.Errorf("control = %#x: want enabled, one message", got)
	}
	if got := w[Address(0, 4, 0, offCommand)]; got&cmdINTxDisable == 0 {
		t.Errorf("command = %#x: INTx still on", got)
	}
}

func TestEnableMSIX(t *testing.T) {
	b, d := withCaps()
	cfg := PortConfig{IO: b}
	bar, off, n := MSIXTable(cfg, d, 0x70)
	if bar != 1 || off != 0x2000 || n != 4 {
		t.Fatalf("table = BAR %d + %#x, %d entries", bar, off, n)
	}
	table := map[uint32]uint32{12: 1} // entry 0 masked, as after reset
	w32 := func(o, v uint32) { table[o] = v }
	if err := EnableMSIX(cfg, d, 0x70, w32, 0xFEE00000, 0x50); err != nil {
		t.Fatal(err)
	}
	if table[0] != 0xFEE00000 || table[4] != 0 || table[8] != 0x50 || table[12] != 0 {
		t.Errorf("entry 0 = %v", table)
	}
	ctl := b.words[Address(0, 4, 0, 0x70)]
	if ctl&msixEnable == 0 || ctl&msixFunctionMsk != 0 {
		t.Errorf("control = %#x: want enabled and unmasked", ctl)
	}
	if EnableMSIX(cfg, d, 0x70, nil, 0, 0) == nil {
		t.Error("no table: no error")
	}
}
