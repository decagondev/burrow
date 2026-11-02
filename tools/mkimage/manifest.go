// Package main implements mkimage, the host tool that assembles disk.img.
//
// manifest.go holds the pure, testable part: the on-disk manifest format.
// It mirrors include/burrow/manifest.h byte for byte.
package main

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"hash/crc32"
)

const (
	SectorSize       = 512
	ManifestLBA      = 1
	ManifestVersion  = 1
	MaxEntries       = 11
	NameLen          = 16
	Stage2LBA        = 2
	Stage2Sectors    = 32
	FirstPayloadLBA  = Stage2LBA + Stage2Sectors
	manifestMagic    = "BRWMNFST"
	entrySize        = 40
	manifestHeadSize = 16
)

// Entry describes one image stored on the disk.
type Entry struct {
	Name      string
	LBA       uint32
	Sectors   uint32
	SizeBytes uint64
	CRC32     uint32
	Flags     uint32
}

// Manifest is the table of contents stored at LBA 1.
type Manifest struct {
	Entries []Entry
}

// SectorsFor rounds a byte count up to whole sectors.
func SectorsFor(n int) uint32 { return uint32((n + SectorSize - 1) / SectorSize) }

// NewEntry builds an entry for data that will be placed at lba.
func NewEntry(name string, lba uint32, data []byte) (Entry, error) {
	if len(name) == 0 || len(name) >= NameLen {
		return Entry{}, fmt.Errorf("entry name %q must be 1..%d bytes", name, NameLen-1)
	}
	return Entry{
		Name:      name,
		LBA:       lba,
		Sectors:   SectorsFor(len(data)),
		SizeBytes: uint64(len(data)),
		CRC32:     crc32.ChecksumIEEE(data),
	}, nil
}

// MarshalBinary encodes the manifest into exactly one 512-byte sector.
func (m Manifest) MarshalBinary() ([]byte, error) {
	if len(m.Entries) == 0 || len(m.Entries) > MaxEntries {
		return nil, fmt.Errorf("manifest needs 1..%d entries, got %d", MaxEntries, len(m.Entries))
	}
	var buf bytes.Buffer
	buf.WriteString(manifestMagic)
	le := binary.LittleEndian
	_ = binary.Write(&buf, le, uint16(ManifestVersion))
	_ = binary.Write(&buf, le, uint16(len(m.Entries)))
	_ = binary.Write(&buf, le, uint32(0))
	for _, e := range m.Entries {
		var name [NameLen]byte
		copy(name[:], e.Name)
		buf.Write(name[:])
		_ = binary.Write(&buf, le, e.LBA)
		_ = binary.Write(&buf, le, e.Sectors)
		_ = binary.Write(&buf, le, e.SizeBytes)
		_ = binary.Write(&buf, le, e.CRC32)
		_ = binary.Write(&buf, le, e.Flags)
	}
	out := make([]byte, SectorSize)
	copy(out, buf.Bytes())
	return out, nil
}

// UnmarshalBinary decodes a manifest sector (used by tests and `mkimage -inspect`).
func (m *Manifest) UnmarshalBinary(b []byte) error {
	if len(b) < SectorSize {
		return errors.New("manifest: short buffer")
	}
	if string(b[:8]) != manifestMagic {
		return errors.New("manifest: bad magic")
	}
	le := binary.LittleEndian
	if v := le.Uint16(b[8:]); v != ManifestVersion {
		return fmt.Errorf("manifest: version %d unsupported", v)
	}
	count := int(le.Uint16(b[10:]))
	if count == 0 || count > MaxEntries {
		return fmt.Errorf("manifest: bad count %d", count)
	}
	m.Entries = m.Entries[:0]
	for i := 0; i < count; i++ {
		p := b[manifestHeadSize+i*entrySize:]
		name := string(bytes.TrimRight(p[:NameLen], "\x00"))
		m.Entries = append(m.Entries, Entry{
			Name:      name,
			LBA:       le.Uint32(p[16:]),
			Sectors:   le.Uint32(p[20:]),
			SizeBytes: le.Uint64(p[24:]),
			CRC32:     le.Uint32(p[32:]),
			Flags:     le.Uint32(p[36:]),
		})
	}
	return nil
}
