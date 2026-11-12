package main

import (
	"bytes"
	"debug/pe"
	"encoding/binary"
	"strings"
	"testing"
)

// testELF builds a small static-PIE ELF: code at 0x1000, data at 0x2000 (with
// a PT_DYNAMIC inside), and a .rela.dyn of (offset, type, addend) triples.
func testELF(dynamic bool, textVaddr uint64, relocs [][3]uint64) []byte {
	le := binary.LittleEndian
	const phoff, nph = 64, 3
	text := bytes.Repeat([]byte{0x90}, 0x40)
	data := make([]byte, 0x80)
	var rela []byte
	for _, r := range relocs {
		rela = le.AppendUint64(rela, r[0])
		rela = le.AppendUint64(rela, r[1])
		rela = le.AppendUint64(rela, r[2])
	}
	shstr := []byte("\x00.rela.dyn\x00.shstrtab\x00")
	out := make([]byte, 0x3000)
	copy(out[0x1000:], text)
	copy(out[0x2000:], data)
	relaOff := len(out)
	out = append(out, rela...)
	strOff := len(out)
	out = append(out, shstr...)
	for len(out)%8 != 0 {
		out = append(out, 0)
	}
	shoff := len(out)
	out = append(out, make([]byte, 3*64)...)

	copy(out, "\x7fELF\x02\x01\x01")
	le.PutUint16(out[16:], 3)    // ET_DYN
	le.PutUint16(out[18:], 0x3E) // x86-64
	le.PutUint32(out[20:], 1)
	le.PutUint64(out[24:], textVaddr+0x10) // entry
	le.PutUint64(out[32:], phoff)
	le.PutUint64(out[40:], uint64(shoff))
	le.PutUint16(out[52:], 64)
	le.PutUint16(out[54:], 56)
	le.PutUint16(out[56:], nph)
	le.PutUint16(out[58:], 64)
	le.PutUint16(out[60:], 3)
	le.PutUint16(out[62:], 2)
	ph := func(i int, typ, flags uint32, off, vaddr, size uint64) {
		p := out[phoff+56*i:]
		le.PutUint32(p[0:], typ)
		le.PutUint32(p[4:], flags)
		le.PutUint64(p[8:], off)
		le.PutUint64(p[16:], vaddr)
		le.PutUint64(p[24:], vaddr)
		le.PutUint64(p[32:], size)
		le.PutUint64(p[40:], size)
		le.PutUint64(p[48:], 0x1000)
	}
	ph(0, 1, 5, 0x1000, textVaddr, uint64(len(text)))
	ph(1, 1, 6, 0x2000, 0x2000, uint64(len(data)))
	dynType := uint32(2) // PT_DYNAMIC
	if !dynamic {
		dynType = 4 // PT_NOTE
	}
	ph(2, dynType, 6, 0x2040, 0x2040, 0x10)
	sh := func(i int, name, typ uint32, off, size, entsize uint64) {
		s := out[shoff+64*i:]
		le.PutUint32(s[0:], name)
		le.PutUint32(s[4:], typ)
		le.PutUint64(s[24:], off)
		le.PutUint64(s[32:], size)
		le.PutUint64(s[48:], 8)
		le.PutUint64(s[56:], entsize)
	}
	sh(1, 1, 4, uint64(relaOff), uint64(len(rela)), 24) // .rela.dyn, SHT_RELA
	sh(2, 11, 3, uint64(strOff), uint64(len(shstr)), 0) // .shstrtab
	return out
}

func TestELFToPE(t *testing.T) {
	img, err := ELFToPE(testELF(true, 0x1000, [][3]uint64{{0x2018, 8, 0x2050}, {0x2010, 8, 0x1020}, {0x2020, 0, 0}}))
	if err != nil {
		t.Fatal(err)
	}
	f, err := pe.NewFile(bytes.NewReader(img))
	if err != nil {
		t.Fatal(err)
	}
	oh := f.OptionalHeader.(*pe.OptionalHeader64)
	if f.Machine != pe.IMAGE_FILE_MACHINE_AMD64 || oh.Subsystem != pe.IMAGE_SUBSYSTEM_EFI_APPLICATION {
		t.Fatalf("machine %#x subsystem %d", f.Machine, oh.Subsystem)
	}
	if oh.AddressOfEntryPoint != 0x1010 || oh.ImageBase != 0 || oh.SizeOfImage != 0x4000 {
		t.Fatalf("entry %#x base %#x size %#x", oh.AddressOfEntryPoint, oh.ImageBase, oh.SizeOfImage)
	}
	var names []string
	for _, s := range f.Sections {
		names = append(names, s.Name)
	}
	if strings.Join(names, " ") != ".text .data .reloc" {
		t.Fatalf("sections %v", names)
	}
	data, _ := f.Section(".data").Data()
	if binary.LittleEndian.Uint64(data[0x10:]) != 0x1020 || binary.LittleEndian.Uint64(data[0x18:]) != 0x2050 {
		t.Fatalf("relocated slots hold %#x, %#x", binary.LittleEndian.Uint64(data[0x10:]), binary.LittleEndian.Uint64(data[0x18:]))
	}
	dir := oh.DataDirectory[peRelocDir]
	rel, _ := f.Section(".reloc").Data()
	want := []byte{0x00, 0x20, 0, 0, 12, 0, 0, 0, 0x10, 0xA0, 0x18, 0xA0} // page 0x2000, size 12, two DIR64
	if dir.VirtualAddress != 0x3000 || dir.Size != uint32(len(want)) || !bytes.Equal(rel[:len(want)], want) {
		t.Fatalf("reloc directory %#x+%d: % x", dir.VirtualAddress, dir.Size, rel[:len(want)])
	}
}

func TestELFToPERefuses(t *testing.T) {
	cases := map[string][]byte{
		"not position independent": testELF(false, 0x1000, nil),
		"absolute relocation":      testELF(true, 0x1000, [][3]uint64{{0x2010, 1, 0}}),
		"segment in the headers":   testELF(true, 0, nil),
		"outside the segments":     testELF(true, 0x1000, [][3]uint64{{0x5000, 8, 0}}),
	}
	for name, elf := range cases {
		if _, err := ELFToPE(elf); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestBaseRelocsPadsAndSplitsPages(t *testing.T) {
	got := baseRelocs([]uint32{0x1008, 0x2000, 0x2ff8})
	want := []byte{0x00, 0x10, 0, 0, 12, 0, 0, 0, 0x08, 0xA0, 0, 0, // one entry + ABSOLUTE padding
		0x00, 0x20, 0, 0, 12, 0, 0, 0, 0x00, 0xA0, 0xF8, 0xAF}
	if !bytes.Equal(got, want) {
		t.Fatalf("% x", got)
	}
}
