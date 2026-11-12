// pe.go turns Burrow's UEFI loader, linked as a static position-independent
// ELF (loader/efi/linker.ld), into the PE32+ EFI application firmware runs
// (W26-S01). binutils' PE linker takes ELF objects but writes no base
// relocations for them, so a loader it made would only work where it was
// linked; here each PT_LOAD becomes a section at the same relative address
// and each R_X86_64_RELATIVE a DIR64 base relocation.
//
// Pure: bytes in, bytes out. It never guesses: any other dynamic relocation,
// a segment that is not page-aligned or overlaps the headers is an error.
// Docs: docs/04-plan/waves/WAVE-26-uefi.md.
package main

import (
	"bytes"
	"debug/elf"
	"encoding/binary"
	"errors"
	"fmt"
	"sort"
)

const (
	peSectionAlign  = 0x1000
	peFileAlign     = 0x200
	peHeaderOffset  = 0x40 // e_lfanew: the PE signature right after the DOS header
	peOptHeaderSize = 240  // PE32+ with 16 data directories
	peSubsystemEFI  = 10   // EFI application
	peMachineAMD64  = 0x8664
	peRelocDir      = 5 // IMAGE_DIRECTORY_ENTRY_BASERELOC
	peRelDir64      = 10
	elfRelRelative  = 8 // R_X86_64_RELATIVE
)

type peSection struct {
	name  string
	rva   uint32
	vsize uint32
	data  []byte
	flags uint32
}

// ELFToPE converts a static-PIE x86-64 ELF into a PE32+ EFI application
// with image base 0 (the firmware relocates it wherever it loads it).
func ELFToPE(image []byte) ([]byte, error) {
	f, err := elf.NewFile(bytes.NewReader(image))
	if err != nil {
		return nil, err
	}
	// ld marks a PIE linked above 0 ET_EXEC; what tells a position-independent
	// link from a fixed one is its PT_DYNAMIC (where .rela.dyn is named).
	if f.Machine != elf.EM_X86_64 || (f.Type != elf.ET_DYN && f.Type != elf.ET_EXEC) || !hasDynamic(f) {
		return nil, errors.New("want a static position-independent x86-64 ELF (linked -pie, with PT_DYNAMIC)")
	}
	var secs []peSection
	for _, p := range f.Progs {
		if p.Type != elf.PT_LOAD {
			continue
		}
		if p.Vaddr%peSectionAlign != 0 || p.Vaddr < peSectionAlign || p.Filesz > p.Memsz {
			return nil, fmt.Errorf("segment at %#x: want page-aligned, above the headers", p.Vaddr)
		}
		data := make([]byte, p.Filesz)
		if _, err := p.ReadAt(data, 0); err != nil {
			return nil, err
		}
		secs = append(secs, peSection{name: sectionName(p.Flags), rva: uint32(p.Vaddr), vsize: uint32(p.Memsz),
			data: data, flags: sectionFlags(p.Flags)})
	}
	if len(secs) == 0 {
		return nil, errors.New("no PT_LOAD segments")
	}
	sort.Slice(secs, func(i, j int) bool { return secs[i].rva < secs[j].rva })
	for i := 1; i < len(secs); i++ {
		if secs[i-1].rva+secs[i-1].vsize > secs[i].rva {
			return nil, fmt.Errorf("segments at %#x and %#x overlap", secs[i-1].rva, secs[i].rva)
		}
	}
	offs, err := applyRelative(f, secs)
	if err != nil {
		return nil, err
	}
	if len(offs) > 0 {
		last := secs[len(secs)-1]
		secs = append(secs, peSection{name: ".reloc", rva: alignUp(last.rva+last.vsize, peSectionAlign),
			data: baseRelocs(offs), flags: 0x42000040}) // initialised data, discardable, read
		secs[len(secs)-1].vsize = uint32(len(secs[len(secs)-1].data))
	}
	return writePE(secs, uint32(f.Entry), len(offs) > 0), nil
}

func hasDynamic(f *elf.File) bool {
	for _, p := range f.Progs {
		if p.Type == elf.PT_DYNAMIC {
			return true
		}
	}
	return false
}

func sectionName(fl elf.ProgFlag) string {
	switch {
	case fl&elf.PF_X != 0:
		return ".text"
	case fl&elf.PF_W != 0:
		return ".data"
	}
	return ".rdata"
}

func sectionFlags(fl elf.ProgFlag) uint32 {
	c := uint32(0x40000000) // readable
	if fl&elf.PF_X != 0 {
		c |= 0x20000000 | 0x20 // executable code
	} else {
		c |= 0x40 // initialised data
	}
	if fl&elf.PF_W != 0 {
		c |= 0x80000000
	}
	return c
}

// applyRelative writes each R_X86_64_RELATIVE's addend (image base 0) into
// its section and returns the relocated offsets, sorted.
func applyRelative(f *elf.File, secs []peSection) ([]uint32, error) {
	rela := f.Section(".rela.dyn")
	if rela == nil {
		return nil, nil
	}
	raw, err := rela.Data()
	if err != nil {
		return nil, err
	}
	var offs []uint32
	for i := 0; i+24 <= len(raw); i += 24 {
		off := binary.LittleEndian.Uint64(raw[i:])
		typ := uint32(binary.LittleEndian.Uint64(raw[i+8:]))
		addend := binary.LittleEndian.Uint64(raw[i+16:])
		if typ == 0 {
			continue
		}
		if typ != elfRelRelative {
			return nil, fmt.Errorf("relocation type %d at %#x: only R_X86_64_RELATIVE can become a base relocation", typ, off)
		}
		s := sectionAt(secs, off)
		if s == nil {
			return nil, fmt.Errorf("relocation at %#x outside every segment's file bytes", off)
		}
		binary.LittleEndian.PutUint64(s.data[off-uint64(s.rva):], addend)
		offs = append(offs, uint32(off))
	}
	sort.Slice(offs, func(i, j int) bool { return offs[i] < offs[j] })
	return offs, nil
}

func sectionAt(secs []peSection, off uint64) *peSection {
	for i := range secs {
		s := &secs[i]
		if off >= uint64(s.rva) && off+8 <= uint64(s.rva)+uint64(len(s.data)) {
			return s
		}
	}
	return nil
}

// baseRelocs groups offsets into one block per 4 KiB page: a page RVA, the
// block's size, then a 16-bit entry each (type DIR64, offset in the page),
// padded with an ABSOLUTE entry to a multiple of four bytes.
func baseRelocs(offs []uint32) []byte {
	var out []byte
	for i := 0; i < len(offs); {
		page := offs[i] &^ 0xFFF
		var ents []uint16
		for ; i < len(offs) && offs[i]&^0xFFF == page; i++ {
			ents = append(ents, peRelDir64<<12|uint16(offs[i]&0xFFF))
		}
		if len(ents)%2 == 1 {
			ents = append(ents, 0)
		}
		out = binary.LittleEndian.AppendUint32(out, page)
		out = binary.LittleEndian.AppendUint32(out, uint32(8+2*len(ents)))
		for _, e := range ents {
			out = binary.LittleEndian.AppendUint16(out, e)
		}
	}
	return out
}

func alignUp(v, a uint32) uint32 { return (v + a - 1) &^ (a - 1) }
