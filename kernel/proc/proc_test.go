package proc_test

import (
	"bytes"
	"encoding/binary"
	"errors"
	"testing"

	"burrow/kernel/proc"
	"burrow/kernel/proc/proctest"
)

var le = binary.LittleEndian

func newMem(t *testing.T) (*proc.Mem, *proctest.Fake) {
	t.Helper()
	nu := proctest.New()
	m, err := proc.NewMem(nu)
	if err != nil {
		t.Fatal(err)
	}
	return m, nu
}

func TestMemMapCopyUnmapClose(t *testing.T) {
	m, nu := newMem(t)
	const va = 0x10000
	if err := m.MapAnon(va, 2*proc.PageSize, proc.ProtWrite); err != nil {
		t.Fatal(err)
	}
	msg := []byte("crosses a page boundary\x00")
	at := uint64(va + proc.PageSize - 5)
	if err := m.CopyOut(at, msg); err != nil {
		t.Fatal(err)
	}
	got := make([]byte, len(msg))
	if err := m.CopyIn(at, got); err != nil || !bytes.Equal(got, msg) {
		t.Fatalf("CopyIn = %q, %v", got, err)
	}
	if s, err := m.CString(at, 64); err != nil || s != "crosses a page boundary" {
		t.Errorf("CString = %q, %v", s, err)
	}
	if err := m.CopyIn(va+2*proc.PageSize, got); !errors.Is(err, proc.ErrFault) {
		t.Errorf("CopyIn past the mapping = %v", err)
	}
	if err := m.MapAnon(0x20000, proc.PageSize, proc.ProtExec); err != nil {
		t.Fatal(err)
	}
	if err := m.CopyOut(0x20000, msg); !errors.Is(err, proc.ErrFault) {
		t.Errorf("CopyOut to a read-only page = %v", err)
	}
	if err := m.MapAnon(0x20000, 1, proc.ProtWrite); err != nil { // widen in place
		t.Fatal(err)
	}
	if p, _ := nu.Prot(m.Space(), 0x20000); p != proc.ProtWrite|proc.ProtExec {
		t.Errorf("prot after widening = %v", p)
	}
	if err := m.Unmap(va, proc.PageSize); err != nil || m.Mapped(va, 1) || !m.Mapped(va+proc.PageSize, 1) {
		t.Errorf("Unmap: %v", err)
	}
	if nu.FramesInUse() != 2 {
		t.Errorf("%d frames in use, want 2", nu.FramesInUse())
	}
	if err := m.Close(); err != nil || nu.FramesInUse() != 0 {
		t.Errorf("Close: %v, %d frames left", err, nu.FramesInUse())
	}
}

type seg struct {
	vaddr, off, memsz uint64
	flags             uint32
	data              []byte
}

// buildELF writes an ELF64 file: header, program headers at 64, segments
// at their offsets (the first may cover the headers, as linkers do).
func buildELF(typ uint16, entry uint64, segs []seg, extra ...uint32) []byte {
	f := make([]byte, 0x4000)
	copy(f, "\x7fELF\x02\x01\x01")
	le.PutUint16(f[16:], typ)
	le.PutUint16(f[18:], 62) // EM_X86_64
	le.PutUint32(f[20:], 1)
	le.PutUint64(f[24:], entry)
	le.PutUint64(f[32:], 64)
	le.PutUint16(f[52:], 64)
	le.PutUint16(f[54:], 56)
	le.PutUint16(f[56:], uint16(len(segs)+len(extra)))
	for i, s := range segs {
		ph := f[64+56*i:]
		le.PutUint32(ph[0:], 1) // PT_LOAD
		le.PutUint32(ph[4:], s.flags)
		le.PutUint64(ph[8:], s.off)
		le.PutUint64(ph[16:], s.vaddr)
		le.PutUint64(ph[32:], uint64(len(s.data)))
		le.PutUint64(ph[40:], s.memsz)
		le.PutUint64(ph[48:], 0x1000)
		if s.off != 0 { // a segment at offset 0 is the headers plus what the test writes after them
			copy(f[s.off:], s.data)
		}
	}
	for i, typ := range extra { // e.g. PT_INTERP
		le.PutUint32(f[64+56*(len(segs)+i):], typ)
	}
	return f
}

const (
	pfX = 1
	pfW = 2
	pfR = 4
)

func TestLoadELF(t *testing.T) {
	code := []byte{0xB8, 60, 0, 0, 0, 0x0F, 0x05}
	file := buildELF(2, 0x400200, []seg{
		{vaddr: 0x400000, off: 0, memsz: 0x200 + 7, flags: pfR | pfX, data: append(make([]byte, 0x200), code...)},
		{vaddr: 0x401ff0, off: 0x2ff0, memsz: 0x20 + 0x3000, flags: pfR | pfW, data: []byte("initialised data")},
	})
	copy(file[0x200:], code) // inside the first segment, after the headers
	m, nu := newMem(t)
	img, err := proc.LoadELF(m, bytes.NewReader(file))
	if err != nil {
		t.Fatal(err)
	}
	if img.Entry != 0x400200 || img.Phdr != 0x400040 || img.Phnum != 2 || img.Phent != 56 || img.End != 0x406000 {
		t.Errorf("Image = %+v", img)
	}
	got := make([]byte, len(code))
	m.CopyIn(0x400200, got)
	if !bytes.Equal(got, code) {
		t.Errorf("code = % x", got)
	}
	data := make([]byte, 16)
	m.CopyIn(0x401ff0, data)
	if string(data) != "initialised data" {
		t.Errorf("data = %q (it straddles a page)", data)
	}
	bss := make([]byte, 0x3000)
	m.CopyIn(0x402000, bss)
	if !bytes.Equal(bss, make([]byte, 0x3000)) {
		t.Error("bss is not zero")
	}
	if p, _ := nu.Prot(m.Space(), 0x400000); p != proc.ProtExec {
		t.Errorf("text prot %v", p)
	}
	if p, _ := nu.Prot(m.Space(), 0x404000); p != proc.ProtWrite {
		t.Errorf("bss prot %v", p)
	}
}

func TestLoadStaticPIE(t *testing.T) {
	file := buildELF(3, 0x100, []seg{{vaddr: 0, off: 0, memsz: 0x1000, flags: pfR | pfX, data: make([]byte, 0x200)}})
	m, _ := newMem(t)
	img, err := proc.LoadELF(m, bytes.NewReader(file))
	if err != nil {
		t.Fatal(err)
	}
	if img.Entry != proc.PIEBase+0x100 || !m.Mapped(proc.PIEBase, proc.PageSize) {
		t.Errorf("static-PIE: %+v", img)
	}
}

func TestLoadELFRejects(t *testing.T) {
	text := []seg{{vaddr: 0x400000, off: 0, memsz: 0x1000, flags: pfR | pfX, data: make([]byte, 0x200)}}
	bad32 := buildELF(2, 0x400000, text)
	bad32[4] = 1
	for name, file := range map[string][]byte{
		"interpreter":  buildELF(2, 0x400000, text, 3),
		"32-bit":       bad32,
		"relocatable":  buildELF(1, 0, text),
		"segment at 0": buildELF(2, 0, []seg{{vaddr: 0, off: 0, memsz: 0x1000, flags: pfR, data: make([]byte, 0x200)}}),
		"no segments":  buildELF(2, 0, nil),
		"not an ELF":   []byte("#!/bin/sh\necho hi\n"),
	} {
		m, _ := newMem(t)
		if _, err := proc.LoadELF(m, bytes.NewReader(file)); err == nil {
			t.Errorf("%s: loaded", name)
		}
	}
}

func TestBuildStack(t *testing.T) {
	m, _ := newMem(t)
	if err := m.MapAnon(proc.StackTop-proc.StackSize, proc.StackSize, proc.ProtWrite); err != nil {
		t.Fatal(err)
	}
	img := proc.Image{Entry: 0x401000, Phdr: 0x400040, Phent: 56, Phnum: 3}
	rnd := [16]byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16}
	sp, err := proc.BuildStack(m, img, []string{"/bin/hello", "-v"}, []string{"HOME=/"}, rnd)
	if err != nil {
		t.Fatal(err)
	}
	if sp%16 != 0 {
		t.Errorf("rsp %#x is not 16-byte aligned", sp)
	}
	word := func(i int) uint64 {
		b := make([]byte, 8)
		m.CopyIn(sp+uint64(8*i), b)
		return le.Uint64(b)
	}
	str := func(va uint64) string { s, _ := m.CString(va, 256); return s }
	if word(0) != 2 || str(word(1)) != "/bin/hello" || str(word(2)) != "-v" || word(3) != 0 ||
		str(word(4)) != "HOME=/" || word(5) != 0 {
		t.Fatalf("argc/argv/envp wrong: %d %q", word(0), str(word(1)))
	}
	aux := map[uint64]uint64{}
	for i := 6; word(i) != 0; i += 2 {
		aux[word(i)] = word(i + 1)
	}
	got := make([]byte, 16)
	m.CopyIn(aux[25], got)
	if aux[9] != img.Entry || aux[3] != img.Phdr || aux[5] != 3 || aux[6] != 4096 ||
		!bytes.Equal(got, rnd[:]) || aux[31] != word(1) {
		t.Errorf("auxv %v", aux)
	}
}
