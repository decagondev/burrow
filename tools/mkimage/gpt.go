// gpt.go wraps an EFI system partition (a FAT32 volume kernel/cmd/mkfat made)
// in a GPT disk, the layout UEFI firmware boots from (W26-S01): a protective
// MBR, the primary header and its 128-entry table, the partition at 1 MiB,
// then the backup table and header in the last sectors. Since W26-S05 data
// volumes (ext2 or FAT32 images) may follow it, each at the next MiB, as
// Linux file system data partitions: make uefi-usb's stick. GUIDs come from
// the partitions' bytes, so the same inputs give the same disk.
//
// Pure: bytes in, bytes out. Docs: docs/04-plan/waves/WAVE-26-uefi.md.
package main

import (
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"hash/crc32"
	"unicode/utf16"
)

const (
	gptHeaderLBA     = 1
	gptEntriesLBA    = 2
	gptEntries       = 128
	gptEntrySize     = 128
	gptEntrySectors  = gptEntries * gptEntrySize / SectorSize // 32
	gptHeaderSize    = 92
	gptESPFirstLBA   = 2048 // 1 MiB, where partitioning tools start
	gptPartitionName = "EFI system partition"
)

// GPTPart is a data partition: its name in the table and its bytes.
type GPTPart struct {
	Name string
	Data []byte
}

// linuxDataGUID is 0FC63DAF-8483-4772-8E79-3D69D8477DE4, a Linux file
// system's partition type, in GPT's byte order.
var linuxDataGUID = [16]byte{0xAF, 0x3D, 0xC6, 0x0F, 0x83, 0x84, 0x72, 0x47,
	0x8E, 0x79, 0x3D, 0x69, 0xD8, 0x47, 0x7D, 0xE4}

const gptAlignSectors = 2048 // data partitions start on a MiB

// espTypeGUID is C12A7328-F81F-11D2-BA4B-00A0C93EC93B in GPT's mixed-endian
// encoding (the first three fields little-endian).
var espTypeGUID = [16]byte{0x28, 0x73, 0x2A, 0xC1, 0x1F, 0xF8, 0xD2, 0x11,
	0xBA, 0x4B, 0x00, 0xA0, 0xC9, 0x3E, 0xC9, 0x3B}

// BuildGPT returns a disk holding esp as its first partition, an EFI
// system partition, and each data volume after it.
func BuildGPT(esp []byte, data ...GPTPart) ([]byte, error) {
	parts := append([]GPTPart{{gptPartitionName, esp}}, data...)
	if len(parts) > gptEntries {
		return nil, fmt.Errorf("%d partitions; a table holds %d", len(parts), gptEntries)
	}
	firsts := make([]uint64, len(parts))
	next := uint64(gptESPFirstLBA)
	for i, p := range parts {
		if len(p.Data) == 0 || len(p.Data)%SectorSize != 0 {
			return nil, fmt.Errorf("partition %q is %d bytes; want whole sectors", p.Name, len(p.Data))
		}
		next = (next + gptAlignSectors - 1) / gptAlignSectors * gptAlignSectors
		firsts[i] = next
		next += uint64(len(p.Data) / SectorSize)
	}
	total := next + gptEntrySectors + 1
	disk := make([]byte, total*SectorSize)
	entries := make([]byte, gptEntries*gptEntrySize)
	for i, p := range parts {
		typ := linuxDataGUID
		if i == 0 {
			typ = espTypeGUID
		}
		putEntry(entries[i*gptEntrySize:], typ, p, firsts[i])
		copy(disk[firsts[i]*SectorSize:], p.Data)
	}
	sum := sha256.Sum256(esp)
	writeTables(disk, total, entries, randomGUID(sum[0:16]))
	return disk, nil
}

// putEntry writes one table entry; its GUID comes from the partition's bytes.
func putEntry(e []byte, typ [16]byte, p GPTPart, first uint64) {
	h := sha256.Sum256(p.Data)
	partGUID := randomGUID(h[16:32])
	copy(e[0:16], typ[:])
	copy(e[16:32], partGUID[:])
	binary.LittleEndian.PutUint64(e[32:], first)
	binary.LittleEndian.PutUint64(e[40:], first+uint64(len(p.Data)/SectorSize)-1)
	for j, u := range utf16.Encode([]rune(p.Name)) {
		if j < 36 {
			binary.LittleEndian.PutUint16(e[56+2*j:], u)
		}
	}
}

// writeTables writes the protective MBR, both headers and both tables.
func writeTables(disk []byte, total uint64, entries []byte, diskGUID [16]byte) {
	last := total - 1
	entriesCRC := crc32.ChecksumIEEE(entries)
	protectiveMBR(disk[:SectorSize], total)
	backupEntries := last - gptEntrySectors
	copy(disk[gptEntriesLBA*SectorSize:], entries)
	copy(disk[backupEntries*SectorSize:], entries)
	hdr := func(my, alt, table uint64) []byte {
		h := make([]byte, gptHeaderSize)
		copy(h, "EFI PART")
		binary.LittleEndian.PutUint32(h[8:], 0x00010000)
		binary.LittleEndian.PutUint32(h[12:], gptHeaderSize)
		binary.LittleEndian.PutUint64(h[24:], my)
		binary.LittleEndian.PutUint64(h[32:], alt)
		binary.LittleEndian.PutUint64(h[40:], gptEntriesLBA+gptEntrySectors)
		binary.LittleEndian.PutUint64(h[48:], backupEntries-1)
		copy(h[56:72], diskGUID[:])
		binary.LittleEndian.PutUint64(h[72:], table)
		binary.LittleEndian.PutUint32(h[80:], gptEntries)
		binary.LittleEndian.PutUint32(h[84:], gptEntrySize)
		binary.LittleEndian.PutUint32(h[88:], entriesCRC)
		binary.LittleEndian.PutUint32(h[16:], crc32.ChecksumIEEE(h)) // with its own field zero
		return h
	}
	copy(disk[gptHeaderLBA*SectorSize:], hdr(gptHeaderLBA, last, gptEntriesLBA))
	copy(disk[last*SectorSize:], hdr(last, gptHeaderLBA, backupEntries))
}

// protectiveMBR marks the whole disk as one partition of type 0xEE, so tools
// that know only MBRs leave it alone.
func protectiveMBR(s []byte, totalSectors uint64) {
	e := s[446:]
	copy(e, []byte{0x00, 0x00, 0x02, 0x00, 0xEE, 0xFF, 0xFF, 0xFF})
	binary.LittleEndian.PutUint32(e[8:], 1)
	size := totalSectors - 1
	if size > 0xFFFFFFFF {
		size = 0xFFFFFFFF
	}
	binary.LittleEndian.PutUint32(e[12:], uint32(size))
	s[510], s[511] = 0x55, 0xAA
}

// randomGUID makes a version 4 GUID from 16 bytes (the variant and version
// bits set as RFC 4122 asks).
func randomGUID(b []byte) [16]byte {
	var g [16]byte
	copy(g[:], b)
	g[7] = g[7]&0x0F | 0x40 // version 4 (the third field is little-endian)
	g[8] = g[8]&0x3F | 0x80 // RFC 4122 variant
	return g
}
