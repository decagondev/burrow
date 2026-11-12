// gpt.go wraps an EFI system partition (a FAT32 volume kernel/cmd/mkfat made)
// in a GPT disk, the layout UEFI firmware boots from (W26-S01): a protective
// MBR, the primary header and its 128-entry table, the partition at 1 MiB,
// then the backup table and header in the last sectors. GUIDs come from the
// partition's bytes, so the same inputs give the same disk.
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

// espTypeGUID is C12A7328-F81F-11D2-BA4B-00A0C93EC93B in GPT's mixed-endian
// encoding (the first three fields little-endian).
var espTypeGUID = [16]byte{0x28, 0x73, 0x2A, 0xC1, 0x1F, 0xF8, 0xD2, 0x11,
	0xBA, 0x4B, 0x00, 0xA0, 0xC9, 0x3E, 0xC9, 0x3B}

// BuildGPT returns a disk holding esp as its one partition, an EFI system partition.
func BuildGPT(esp []byte) ([]byte, error) {
	if len(esp) == 0 || len(esp)%SectorSize != 0 {
		return nil, fmt.Errorf("partition is %d bytes; want whole sectors", len(esp))
	}
	espSectors := uint64(len(esp) / SectorSize)
	total := gptESPFirstLBA + espSectors + gptEntrySectors + 1
	last := total - 1
	disk := make([]byte, total*SectorSize)

	sum := sha256.Sum256(esp)
	diskGUID, partGUID := randomGUID(sum[0:16]), randomGUID(sum[16:32])
	entries := make([]byte, gptEntries*gptEntrySize)
	copy(entries[0:16], espTypeGUID[:])
	copy(entries[16:32], partGUID[:])
	binary.LittleEndian.PutUint64(entries[32:], gptESPFirstLBA)
	binary.LittleEndian.PutUint64(entries[40:], gptESPFirstLBA+espSectors-1)
	for i, u := range utf16.Encode([]rune(gptPartitionName)) {
		binary.LittleEndian.PutUint16(entries[56+2*i:], u)
	}
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
	copy(disk[gptESPFirstLBA*SectorSize:], esp)
	return disk, nil
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
