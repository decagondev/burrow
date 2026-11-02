package proc

import (
	"debug/elf"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
)

// PIEBase is where a static-PIE (ET_DYN) program is loaded.
const PIEBase = 0x400000

// Image is what loading an ELF tells the entry stack and brk.
type Image struct {
	Entry, Phdr, Phent, Phnum uint64
	End                       uint64 // page-aligned end of the highest segment: brk starts here
}

// LoadELF maps the PT_LOAD segments of a static x86-64 executable (ET_EXEC,
// or ET_DYN without an interpreter: static-PIE at PIEBase) into m.
func LoadELF(m *Mem, r io.ReaderAt) (Image, error) {
	f, err := elf.NewFile(r)
	if err != nil {
		return Image{}, fmt.Errorf("elf: %w", err)
	}
	if f.Class != elf.ELFCLASS64 || f.Machine != elf.EM_X86_64 || f.Data != elf.ELFDATA2LSB {
		return Image{}, errors.New("elf: not an x86-64 little-endian 64-bit program")
	}
	var base uint64
	switch f.Type {
	case elf.ET_EXEC:
	case elf.ET_DYN:
		base = PIEBase
	default:
		return Image{}, fmt.Errorf("elf: type %v is not an executable", f.Type)
	}
	img := Image{Entry: base + f.Entry, Phent: 56, Phnum: uint64(len(f.Progs))}
	phoff, err := phdrOffset(r)
	if err != nil {
		return Image{}, err
	}
	for _, p := range f.Progs {
		switch p.Type {
		case elf.PT_INTERP:
			return Image{}, errors.New("elf: dynamically linked (has an interpreter): only static programs run")
		case elf.PT_LOAD:
			if err := loadSegment(m, p, base); err != nil {
				return Image{}, err
			}
			if phoff >= p.Off && phoff < p.Off+p.Filesz {
				img.Phdr = base + p.Vaddr + (phoff - p.Off)
			}
			img.End = max(img.End, pageUp(base+p.Vaddr+p.Memsz))
		}
	}
	if img.End == 0 {
		return Image{}, errors.New("elf: no loadable segment")
	}
	return img, nil
}

func loadSegment(m *Mem, p *elf.Prog, base uint64) error {
	if p.Memsz == 0 {
		return nil
	}
	if p.Filesz > p.Memsz {
		return errors.New("elf: segment larger in the file than in memory")
	}
	va := base + p.Vaddr
	if va < PageSize || va >= UserTop || p.Memsz > UserTop-va {
		return fmt.Errorf("elf: segment at %#x outside user space", va)
	}
	var prot Prot
	if p.Flags&elf.PF_W != 0 {
		prot |= ProtWrite
	}
	if p.Flags&elf.PF_X != 0 {
		prot |= ProtExec
	}
	if err := m.MapAnon(va, p.Memsz, prot); err != nil {
		return fmt.Errorf("elf: map %#x: %w", va, err)
	}
	data := make([]byte, p.Filesz)
	if _, err := p.ReadAt(data, 0); err != nil && !errors.Is(err, io.EOF) {
		return fmt.Errorf("elf: read segment: %w", err)
	}
	return m.load(va, data) // the bss beyond Filesz stays zero: fresh frames
}

// phdrOffset reads e_phoff (debug/elf does not expose it).
func phdrOffset(r io.ReaderAt) (uint64, error) {
	var b [8]byte
	if _, err := r.ReadAt(b[:], 0x20); err != nil {
		return 0, fmt.Errorf("elf: header: %w", err)
	}
	return binary.LittleEndian.Uint64(b[:]), nil
}
