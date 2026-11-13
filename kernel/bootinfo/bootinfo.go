// Package bootinfo parses the boot_info v4 block and the boot module table
// the loader leaves for the nucleus (include/burrow/boot_info.h,
// docs/02-architecture/BOOT_PROTOCOL.md). Pure: it reads byte slices, so
// the layout is checked on the host; arch/amd64 supplies the bytes.
package bootinfo

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
)

// Layout of the C structs. Changing one means changing boot_info.h, its
// mirrors (boot/boot_abi.inc, tools/mkimage) and this block together.
const (
	Magic   = 0x4F464E49544F4F42 // "BOOTINFO" little-endian
	Version = 4                  // 3: the framebuffer (W21-S02); 4: rsdp_addr (W26-S02)

	offMagic       = 0x00
	offVersion     = 0x08
	offBootDrive   = 0x10
	offPhysmap     = 0x48
	offModulesAddr = 0x50
	offModuleCount = 0x58
	offFbAddr      = 0x30 // framebuffer (W21-S02): base, width, height, pitch, bpp
	offFbWidth     = 0x38
	offFbHeight    = 0x3C
	offFbPitch     = 0x40
	offFbBPP       = 0x44
	offFbFields    = 0x60 // red size, pos; green size, pos; blue size, pos (VBE's order)
	offFbSource    = 0x66
	offRSDP        = 0x68 // v4 (W26-S02): the ACPI RSDP UEFI firmware named, else 0
	FbSourceVBE    = 1
	FbSourceGOP    = 2 // the UEFI loader (W26-S04)
	InfoBytes      = 0x80

	ModuleBytes   = 32 // name[16], phys_start u64, size_bytes u64
	moduleNameLen = 16
	MaxModules    = 16
)

// Info is the part of boot_info v4 the Go kernel uses.
type Info struct {
	BootDrive   uint32
	PhysmapBase uint64
	ModulesAddr uint64 // physical address of the module table
	ModuleCount int
	Framebuffer Framebuffer // Source 0: none (the kernel sets a DISPI mode itself)
	RSDP        uint64      // physical; 0 when the firmware was a BIOS (W26-S02)
}

// Field is one colour channel of a pixel: Size bits from bit Pos up.
type Field struct{ Size, Pos uint8 }

// Framebuffer is the display mode stage2 set through VBE and the nucleus
// checked (nucleus/fbdesc.c); it clears Source when the check fails.
type Framebuffer struct {
	Source                    uint8
	Phys                      uint64
	Width, Height, Pitch, BPP int
	Red, Green, Blue          Field
}

// Module is one physical range the loader filled: a payload from the disk
// (kernel, initrd, kargs, ...) or the nucleus's own bookkeeping.
type Module struct {
	Name       string
	Phys, Size uint64
}

func (m Module) String() string {
	return fmt.Sprintf("%-14s %8d bytes at %#x", m.Name, m.Size, m.Phys)
}

var le = binary.LittleEndian

// Parse checks the magic and version and decodes the fields above.
func Parse(b []byte) (Info, error) {
	if len(b) < InfoBytes {
		return Info{}, fmt.Errorf("bootinfo: %d bytes, want %d", len(b), InfoBytes)
	}
	if le.Uint64(b[offMagic:]) != Magic {
		return Info{}, errors.New("bootinfo: bad magic")
	}
	if v := le.Uint32(b[offVersion:]); v != Version {
		return Info{}, fmt.Errorf("bootinfo: version %d, want %d", v, Version)
	}
	n := int(le.Uint32(b[offModuleCount:]))
	if n > MaxModules {
		return Info{}, fmt.Errorf("bootinfo: %d modules, at most %d", n, MaxModules)
	}
	return Info{
		BootDrive:   le.Uint32(b[offBootDrive:]),
		PhysmapBase: le.Uint64(b[offPhysmap:]),
		ModulesAddr: le.Uint64(b[offModulesAddr:]),
		ModuleCount: n,
		Framebuffer: parseFramebuffer(b),
		RSDP:        le.Uint64(b[offRSDP:]),
	}, nil
}

func parseFramebuffer(b []byte) Framebuffer {
	if src := b[offFbSource]; src != FbSourceVBE && src != FbSourceGOP {
		return Framebuffer{}
	}
	f := b[offFbFields:]
	return Framebuffer{
		Source: b[offFbSource],
		Phys:   le.Uint64(b[offFbAddr:]),
		Width:  int(le.Uint32(b[offFbWidth:])),
		Height: int(le.Uint32(b[offFbHeight:])),
		Pitch:  int(le.Uint32(b[offFbPitch:])),
		BPP:    int(le.Uint32(b[offFbBPP:])),
		Red:    Field{f[0], f[1]},
		Green:  Field{f[2], f[3]},
		Blue:   Field{f[4], f[5]},
	}
}

// ParseModules decodes a module table of n entries.
func ParseModules(b []byte, n int) ([]Module, error) {
	if len(b) < n*ModuleBytes {
		return nil, fmt.Errorf("bootinfo: module table is %d bytes, want %d", len(b), n*ModuleBytes)
	}
	mods := make([]Module, n)
	for i := range mods {
		e := b[i*ModuleBytes:]
		name, _, _ := bytes.Cut(e[:moduleNameLen], []byte{0})
		mods[i] = Module{
			Name: string(name),
			Phys: le.Uint64(e[moduleNameLen:]),
			Size: le.Uint64(e[moduleNameLen+8:]),
		}
	}
	return mods, nil
}

// Find returns the first module with that name.
func Find(mods []Module, name string) (Module, bool) {
	for _, m := range mods {
		if m.Name == name {
			return m, true
		}
	}
	return Module{}, false
}
