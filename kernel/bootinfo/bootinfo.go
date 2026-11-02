// Package bootinfo parses the boot_info v2 block and the boot module table
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
	Version = 2

	offMagic       = 0x00
	offVersion     = 0x08
	offBootDrive   = 0x10
	offPhysmap     = 0x48
	offModulesAddr = 0x50
	offModuleCount = 0x58
	InfoBytes      = 0x80

	ModuleBytes   = 32 // name[16], phys_start u64, size_bytes u64
	moduleNameLen = 16
	MaxModules    = 16
)

// Info is the part of boot_info v2 the Go kernel uses.
type Info struct {
	BootDrive   uint32
	PhysmapBase uint64
	ModulesAddr uint64 // physical address of the module table
	ModuleCount int
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
	}, nil
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
