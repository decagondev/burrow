package proc

// elf.go - loading a static x86-64 ELF program (W9; lazily since W18-S02):
// the whole pages of each PT_LOAD segment's file contents become a
// file-backed private area (area.go) that fills at first touch, so a
// program costs the pages it uses; the partial pages at a segment's edges
// are copied at once, since the bytes past a segment's end in the file are
// not its own and must read as zero; the bss is mapped at once (a fault is
// a round trip through the dispatcher: a lazy bss cost st 0.17 s).
// Never keeps the program's bytes in Go memory: the area reads the file.
// Docs: docs/04-plan/waves/WAVE-18-foundations.md.

import (
	"bytes"
	"debug/elf"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"io/fs"
)

// PIEBase is where a static-PIE (ET_DYN) program is loaded.
const PIEBase = 0x400000

// Image is what loading an ELF tells the entry stack and brk.
type Image struct {
	Entry, Phdr, Phent, Phnum uint64
	End                       uint64 // page-aligned end of the highest segment: brk starts here
}

// openProgram opens file for execve: a reader over it (the file itself
// when its file system can read at an offset, as every Burrow one can) and
// its first scriptLine bytes, for "#!".
func (s *System) openProgram(file string) (io.ReaderAt, []byte, error) {
	f, err := s.files.Open(file)
	if err != nil {
		return nil, nil, err
	}
	if st, err := f.Stat(); err != nil || st.IsDir() {
		f.Close()
		return nil, nil, fs.ErrPermission // a directory: EACCES, as Linux says
	}
	r, isR := f.(io.ReaderAt)
	if !isR { // read it whole after all
		data, err := io.ReadAll(f)
		f.Close()
		if err != nil {
			return nil, nil, err
		}
		r = bytes.NewReader(data)
	}
	head := make([]byte, scriptLine)
	n, err := r.ReadAt(head, 0)
	if err != nil && !errors.Is(err, io.EOF) {
		return nil, nil, err
	}
	return r, head[:n], nil
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
			if err := loadSegment(m, r, p, base); err != nil {
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

func loadSegment(m *Mem, r io.ReaderAt, p *elf.Prog, base uint64) error {
	if p.Memsz == 0 {
		return nil
	}
	if p.Filesz > p.Memsz {
		return errors.New("elf: segment larger in the file than in memory")
	}
	va := base + p.Vaddr
	if va < PageSize || va >= UserTop || p.Memsz > UserTop-va || va%PageSize != p.Off%PageSize {
		return fmt.Errorf("elf: segment at %#x outside user space or misaligned", va)
	}
	var prot Prot
	if p.Flags&elf.PF_W != 0 {
		prot |= ProtWrite
	}
	if p.Flags&elf.PF_X != 0 {
		prot |= ProtExec
	}
	fileEnd, memEnd := va+p.Filesz, va+p.Memsz
	lo, hi := pageUp(va), pageDown(fileEnd) // the whole pages of file contents
	if lo >= hi {
		lo, hi = fileEnd, fileEnd // none: copy it all
	}
	// the edges, copied now: [va, lo) and [hi, fileEnd)
	for _, part := range [][2]uint64{{va, min(lo, fileEnd)}, {max(hi, va), fileEnd}} {
		if part[0] < part[1] {
			if err := copySegment(m, r, p, va, part[0], part[1], prot); err != nil {
				return err
			}
		}
	}
	if lo < hi {
		if err := m.reserveProgram(lo, hi-lo, prot, r, int64(p.Off+(lo-va))); err != nil {
			return fmt.Errorf("elf: map %#x: %w", lo, err)
		}
	}
	bss := pageUp(fileEnd) // zeros past the last file page...
	if p.Filesz == 0 {
		bss = pageDown(va) // ...or from the segment's first page, when it has no file bytes
	}
	if bss < memEnd {
		if err := m.MapAnon(bss, pageUp(memEnd)-bss, prot); err != nil {
			return fmt.Errorf("elf: bss at %#x: %w", bss, err)
		}
	}
	return nil
}

// copySegment backs [from, to) of segment p (at va) with fresh frames and
// copies its file bytes in; the rest of those pages stays zero.
func copySegment(m *Mem, r io.ReaderAt, p *elf.Prog, va, from, to uint64, prot Prot) error {
	if err := m.MapAnon(pageDown(from), pageUp(to)-pageDown(from), prot); err != nil {
		return fmt.Errorf("elf: map %#x: %w", from, err)
	}
	data := make([]byte, to-from)
	if _, err := r.ReadAt(data, int64(p.Off+(from-va))); err != nil && !errors.Is(err, io.EOF) {
		return fmt.Errorf("elf: read segment: %w", err)
	}
	return m.load(from, data)
}

// phdrOffset reads e_phoff (debug/elf does not expose it).
func phdrOffset(r io.ReaderAt) (uint64, error) {
	var b [8]byte
	if _, err := r.ReadAt(b[:], 0x20); err != nil {
		return 0, fmt.Errorf("elf: header: %w", err)
	}
	return binary.LittleEndian.Uint64(b[:]), nil
}
