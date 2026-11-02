// Package pci enumerates PCI functions through configuration space (E06.F07).
// Enumerate is pure: it reads through the ConfigSpace interface, so tests
// feed it a fake bus. PortConfig is configuration mechanism #1 (ports
// 0xCF8/0xCFC), which every PC chipset and QEMU's i440FX provide.
package pci

import (
	"fmt"

	"burrow/kernel/hal"
)

// ConfigSpace reads one aligned 32-bit word of a function's 256-byte header.
type ConfigSpace interface {
	Read32(bus, dev, fn, off uint8) uint32
}

const (
	portAddress uint16 = 0xCF8
	portData    uint16 = 0xCFC
	enableBit          = 1 << 31

	offID     = 0x00 // vendor 15:0, device 31:16
	offClass  = 0x08 // revision 7:0, prog-if 15:8, subclass 23:16, class 31:24
	offHeader = 0x0C // header type 23:16 (bit 23 = multi-function)
	offBAR0   = 0x10 // six base address registers, 4 bytes each
	offIRQ    = 0x3C // interrupt line 7:0

	noVendor = 0xFFFF
)

// PortConfig is mechanism #1.
type PortConfig struct{ IO hal.PortIO32 }

func (p PortConfig) Read32(bus, dev, fn, off uint8) uint32 {
	p.IO.Out32(portAddress, Address(bus, dev, fn, off))
	return p.IO.In32(portData)
}

// Address is the value written to 0xCF8 for one word.
func Address(bus, dev, fn, off uint8) uint32 {
	return enableBit | uint32(bus)<<16 | uint32(dev&0x1F)<<11 | uint32(fn&7)<<8 | uint32(off&0xFC)
}

// Present reports whether mechanism #1 is there: the address port reads
// back what was written.
func Present(io hal.PortIO32) bool {
	io.Out32(portAddress, enableBit)
	return io.In32(portAddress) == enableBit
}

// Device is one PCI function.
type Device struct {
	Bus, Dev, Fn            uint8
	Vendor, ID              uint16
	Class, Subclass, ProgIF uint8
	Revision, IRQLine       uint8
	BAR                     [6]uint32
}

// MemBase is a memory BAR's address (flag bits cleared); 0 for an I/O BAR.
func (d Device) MemBase(i int) uint64 {
	if d.BAR[i]&1 != 0 {
		return 0
	}
	return uint64(d.BAR[i] &^ 0xF)
}

func (d Device) String() string {
	return fmt.Sprintf("%02x:%02x.%x %04x:%04x %s (class %02x.%02x.%02x, irq %d)",
		d.Bus, d.Dev, d.Fn, d.Vendor, d.ID, ClassName(d.Class, d.Subclass),
		d.Class, d.Subclass, d.ProgIF, d.IRQLine)
}

// Enumerate scans every bus and device; functions 1-7 only where function 0
// says it is multi-function. A brute-force scan (8192 probes) needs no
// bridge handling and takes a few milliseconds.
func Enumerate(cs ConfigSpace) []Device {
	var out []Device
	for bus := 0; bus < 256; bus++ {
		for dev := uint8(0); dev < 32; dev++ {
			d, ok := read(cs, uint8(bus), dev, 0)
			if !ok {
				continue
			}
			out = append(out, d)
			if cs.Read32(uint8(bus), dev, 0, offHeader)&(1<<23) == 0 {
				continue
			}
			for fn := uint8(1); fn < 8; fn++ {
				if d, ok := read(cs, uint8(bus), dev, fn); ok {
					out = append(out, d)
				}
			}
		}
	}
	return out
}

func read(cs ConfigSpace, bus, dev, fn uint8) (Device, bool) {
	id := cs.Read32(bus, dev, fn, offID)
	if uint16(id) == noVendor {
		return Device{}, false
	}
	class := cs.Read32(bus, dev, fn, offClass)
	var bars [6]uint32
	for i := range bars {
		bars[i] = cs.Read32(bus, dev, fn, uint8(offBAR0+4*i))
	}
	return Device{
		BAR: bars,
		Bus: bus, Dev: dev, Fn: fn,
		Vendor: uint16(id), ID: uint16(id >> 16),
		Class: uint8(class >> 24), Subclass: uint8(class >> 16), ProgIF: uint8(class >> 8),
		Revision: uint8(class), IRQLine: uint8(cs.Read32(bus, dev, fn, offIRQ)),
	}, true
}

// ClassName names the common classes; the rest print as numbers only.
func ClassName(class, sub uint8) string {
	switch uint16(class)<<8 | uint16(sub) {
	case 0x0101:
		return "ide controller"
	case 0x0106:
		return "sata controller"
	case 0x0200:
		return "ethernet"
	case 0x0300:
		return "vga"
	case 0x0600:
		return "host bridge"
	case 0x0601:
		return "isa bridge"
	case 0x0604:
		return "pci bridge"
	case 0x0680:
		return "bridge"
	case 0x0C03:
		return "usb controller"
	}
	return "device"
}
