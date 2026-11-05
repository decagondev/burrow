package pci

import (
	"strings"
	"testing"
)

// fakeBus is mechanism #1 over a table of functions: Out32 to 0xCF8 picks a
// word, In32 from 0xCFC returns it (0xFFFFFFFF where nothing answers).
type fakeBus struct {
	addr  uint32
	words map[uint32]uint32
	reads int
}

func (b *fakeBus) Out32(port uint16, v uint32) {
	if port == portAddress {
		b.addr = v
	}
}

func (b *fakeBus) In32(port uint16) uint32 {
	switch port {
	case portAddress:
		return b.addr
	case portData:
		b.reads++
		if w, ok := b.words[b.addr]; ok {
			return w
		}
	}
	return 0xFFFFFFFF
}

func (b *fakeBus) add(bus, dev, fn uint8, vendor, id uint16, class uint32, multi bool, irq uint8) {
	b.words[Address(bus, dev, fn, offID)] = uint32(id)<<16 | uint32(vendor)
	b.words[Address(bus, dev, fn, offClass)] = class
	if multi {
		b.words[Address(bus, dev, fn, offHeader)] = 1 << 23
	}
	b.words[Address(bus, dev, fn, offIRQ)] = uint32(irq)
}

// QEMU's i440FX machine as seen at boot.
func qemuPC() *fakeBus {
	b := &fakeBus{words: map[uint32]uint32{}}
	b.add(0, 0, 0, 0x8086, 0x1237, 0x06000002, false, 0)
	b.add(0, 1, 0, 0x8086, 0x7000, 0x06010000, true, 0)
	b.add(0, 1, 1, 0x8086, 0x7010, 0x01018000, false, 0)
	b.add(0, 1, 3, 0x8086, 0x7113, 0x06800003, false, 9)
	b.add(0, 2, 0, 0x1234, 0x1111, 0x03000002, false, 0)
	b.add(0, 3, 0, 0x8086, 0x100e, 0x02000003, false, 11)
	b.add(0, 4, 2, 0xdead, 0xbeef, 0, false, 0) // fn 2 of a device with no fn 0: never seen
	return b
}

func TestEnumerateQEMU(t *testing.T) {
	b := qemuPC()
	var got []string
	for _, d := range Enumerate(PortConfig{IO: b}) {
		got = append(got, d.String())
	}
	want := []string{
		"00:00.0 8086:1237 host bridge (class 06.00.00, irq 0)",
		"00:01.0 8086:7000 isa bridge (class 06.01.00, irq 0)",
		"00:01.1 8086:7010 ide controller (class 01.01.80, irq 0)",
		"00:01.3 8086:7113 bridge (class 06.80.00, irq 9)",
		"00:02.0 1234:1111 vga (class 03.00.00, irq 0)",
		"00:03.0 8086:100e ethernet (class 02.00.00, irq 11)",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("Enumerate:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	if b.reads > 256*32+16*10 { // every slot once, then about 10 words per function found
		t.Errorf("%d config reads: functions 1-7 of single-function devices were probed", b.reads)
	}
}

func TestAddressAndPresent(t *testing.T) {
	if got := Address(0, 1, 3, 0x3E); got != 0x80000B3C {
		t.Errorf("Address = %#x, want 0x80000b3c (offset rounded down to a word)", got)
	}
	if !Present(&fakeBus{words: map[uint32]uint32{}}) {
		t.Error("Present = false for a bus whose address port reads back")
	}
}

func TestBARsAndFind(t *testing.T) {
	b := qemuPC()
	b.words[Address(0, 2, 0, offBAR0)] = 0xFD000008    // prefetchable memory
	b.words[Address(0, 1, 1, offBAR0+16)] = 0x0000C041 // I/O BAR 4 of the IDE controller
	devs := Enumerate(PortConfig{IO: b})
	vga, ok := Find(devs, 0x1234, 0x1111)
	if !ok || vga.MemBase(0) != 0xFD000000 {
		t.Fatalf("vga = %+v, %v", vga, ok)
	}
	ide, _ := Find(devs, 0x8086, 0x7010)
	if ide.MemBase(4) != 0 || ide.BAR[4] != 0xC041 {
		t.Errorf("an I/O BAR is not memory: %#x", ide.BAR[4])
	}
	if _, ok := Find(devs, 0xdead, 0xbeef); ok {
		t.Error("found a device that was never enumerated")
	}
}

// W22-S04: a 64-bit memory BAR takes its high half from the next one.
func TestMemBase64(t *testing.T) {
	d := Device{BAR: [6]uint32{4: 0xFE00000C, 5: 0x1}}
	if got := d.MemBase64(4); got != 0x1_FE000000 {
		t.Errorf("64-bit BAR: %#x", got)
	}
	d = Device{BAR: [6]uint32{0: 0xFD000008, 1: 0x1}}
	if got := d.MemBase64(0); got != 0xFD000000 {
		t.Errorf("a 32-bit BAR took the next one's bits: %#x", got)
	}
}
