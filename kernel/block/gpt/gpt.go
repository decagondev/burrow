// Package gpt reads a GUID partition table (W26-S04): the UEFI images'
// boot disks are GPT disks whose EFI system partition holds the loader and
// the payloads (tools/mkimage/gpt.go writes them), and S05's USB stick puts
// the data volumes beside it. Read checks the primary header and the entry
// array against their CRCs and every partition against the disk's size;
// Partition.Device reads one partition as a block.Device (and writes it
// when the disk can be written).
//
// It never writes outside a partition and never trusts a field it has not checked.
// UEFI 2.10, 5.3. Docs: docs/04-plan/waves/WAVE-26-uefi.md (S04, PIT-99).
package gpt

import (
	"encoding/binary"
	"errors"
	"fmt"
	"hash/crc32"
	"strings"
	"unicode/utf16"

	"burrow/kernel/block"
)

const (
	headerLBA     = 1
	headerMinSize = 92
	entryMinSize  = 128
	maxEntries    = 1024
	nameOffset    = 56
	nameBytes     = 72 // 36 UTF-16 code units
)

// ESPType is C12A7328-F81F-11D2-BA4B-00A0C93EC93B in GPT's mixed-endian bytes.
var ESPType = [16]byte{0x28, 0x73, 0x2A, 0xC1, 0x1F, 0xF8, 0xD2, 0x11,
	0xBA, 0x4B, 0x00, 0xA0, 0xC9, 0x3E, 0xC9, 0x3B}

// ErrNotGPT means sector 1 does not start with "EFI PART".
var ErrNotGPT = errors.New("gpt: no GPT header")

var le = binary.LittleEndian

// Partition is one used entry: its number (the entry's, from 1, as Linux
// names partitions), type, name and inclusive sector range.
type Partition struct {
	Number      int
	Type        [16]byte
	Name        string
	First, Last uint64
}

// IsESP reports whether p is an EFI system partition.
func (p Partition) IsESP() bool { return p.Type == ESPType }

// Table is a disk's partitions in entry order.
type Table []Partition

// ESP is the first EFI system partition.
func (t Table) ESP() (Partition, bool) {
	for _, p := range t {
		if p.IsESP() {
			return p, true
		}
	}
	return Partition{}, false
}

// Read reads d's primary GPT.
func Read(d block.Device) (Table, error) {
	ss := d.SectorSize()
	h := make([]byte, ss)
	if err := d.ReadSectors(headerLBA, h); err != nil {
		return nil, err
	}
	if string(h[:8]) != "EFI PART" {
		return nil, ErrNotGPT
	}
	size := le.Uint32(h[12:])
	if size < headerMinSize || int(size) > ss {
		return nil, fmt.Errorf("gpt: header size %d", size)
	}
	hdr := append([]byte(nil), h[:size]...)
	le.PutUint32(hdr[16:], 0)
	if crc32.ChecksumIEEE(hdr) != le.Uint32(h[16:]) {
		return nil, errors.New("gpt: header CRC mismatch")
	}
	entries, err := readEntries(d, h)
	if err != nil {
		return nil, err
	}
	return parseEntries(entries, int(le.Uint32(h[84:])), d.Sectors())
}

func readEntries(d block.Device, h []byte) ([]byte, error) {
	lba, n, esize := le.Uint64(h[72:]), le.Uint32(h[80:]), le.Uint32(h[84:])
	if n == 0 || n > maxEntries || esize < entryMinSize || esize%8 != 0 || esize > 4096 {
		return nil, fmt.Errorf("gpt: %d entries of %d bytes", n, esize)
	}
	ss := uint64(d.SectorSize())
	bytes := uint64(n) * uint64(esize)
	sectors := (bytes + ss - 1) / ss
	if lba < 2 || lba+sectors > d.Sectors() {
		return nil, fmt.Errorf("gpt: entries at LBA %d past the disk", lba)
	}
	buf := make([]byte, sectors*ss)
	if err := d.ReadSectors(lba, buf); err != nil {
		return nil, err
	}
	if crc32.ChecksumIEEE(buf[:bytes]) != le.Uint32(h[88:]) {
		return nil, errors.New("gpt: partition entries CRC mismatch")
	}
	return buf[:bytes], nil
}

func parseEntries(b []byte, esize int, diskSectors uint64) (Table, error) {
	var t Table
	for off := 0; off+esize <= len(b); off += esize {
		e := b[off : off+esize]
		p := Partition{Number: off/esize + 1}
		copy(p.Type[:], e[:16])
		if p.Type == ([16]byte{}) {
			continue // unused
		}
		p.First, p.Last = le.Uint64(e[32:]), le.Uint64(e[40:])
		if p.Last < p.First || p.Last >= diskSectors {
			return nil, fmt.Errorf("gpt: partition %d spans LBA %d-%d of %d", off/esize, p.First, p.Last, diskSectors)
		}
		p.Name = name(e[nameOffset : nameOffset+nameBytes])
		t = append(t, p)
	}
	return t, nil
}

func name(b []byte) string {
	u := make([]uint16, len(b)/2)
	for i := range u {
		u[i] = le.Uint16(b[2*i:])
	}
	return strings.TrimRight(string(utf16.Decode(u)), "\x00")
}

// Device reads p on d as a disk of its own (sector 0 = p.First). When d
// can be written so can the partition (W26-S05: the USB stick's root and
// data volumes), never outside its sectors.
func (p Partition) Device(d block.Device) block.Device {
	s := &slice{d: d, first: p.First, n: p.Last - p.First + 1}
	if w, ok := d.(block.Writer); ok {
		return &writableSlice{slice: s, w: w}
	}
	return s
}

type slice struct {
	d        block.Device
	first, n uint64
}

func (s *slice) SectorSize() int { return s.d.SectorSize() }
func (s *slice) Sectors() uint64 { return s.n }

func (s *slice) ReadSectors(lba uint64, buf []byte) error {
	if err := block.CheckRange(s, lba, buf); err != nil {
		return err
	}
	return s.d.ReadSectors(s.first+lba, buf)
}

type writableSlice struct {
	*slice
	w block.Writer
}

func (s *writableSlice) WriteSectors(lba uint64, buf []byte) error {
	if err := block.CheckRange(s, lba, buf); err != nil {
		return err
	}
	return s.w.WriteSectors(s.first+lba, buf)
}

func (s *writableSlice) Flush() error { return s.w.Flush() }
