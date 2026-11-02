package main

import (
	"errors"
	"fmt"
)

// Payload is a named blob to place on disk after stage2.
type Payload struct {
	Name string
	Data []byte
}

// BuildImage lays out: stage1 | manifest | stage2 slot | payloads... | padding.
// Pure function: bytes in, bytes out. All file I/O lives in main.go.
func BuildImage(stage1, stage2 []byte, payloads []Payload, minSize int) ([]byte, Manifest, error) {
	if len(stage1) != SectorSize {
		return nil, Manifest{}, fmt.Errorf("stage1 must be exactly %d bytes, got %d", SectorSize, len(stage1))
	}
	if stage1[510] != 0x55 || stage1[511] != 0xAA {
		return nil, Manifest{}, errors.New("stage1 is missing the 0x55AA boot signature")
	}
	if len(stage2) > Stage2Sectors*SectorSize {
		return nil, Manifest{}, fmt.Errorf("stage2 is %d bytes; slot holds %d", len(stage2), Stage2Sectors*SectorSize)
	}

	var m Manifest
	s2, err := NewEntry("stage2", Stage2LBA, stage2)
	if err != nil {
		return nil, m, err
	}
	m.Entries = append(m.Entries, s2)

	lba := uint32(FirstPayloadLBA)
	seen := map[string]bool{"stage2": true}
	for _, p := range payloads {
		if seen[p.Name] {
			return nil, m, fmt.Errorf("duplicate payload %q", p.Name)
		}
		seen[p.Name] = true
		e, err := NewEntry(p.Name, lba, p.Data)
		if err != nil {
			return nil, m, err
		}
		m.Entries = append(m.Entries, e)
		lba += e.Sectors
	}

	mf, err := m.MarshalBinary()
	if err != nil {
		return nil, m, err
	}

	size := int(lba) * SectorSize
	if size < minSize {
		size = minSize
	}
	img := make([]byte, size)
	copy(img[0:], stage1)
	copy(img[ManifestLBA*SectorSize:], mf)
	copy(img[Stage2LBA*SectorSize:], stage2)
	for i, p := range payloads {
		copy(img[int(m.Entries[i+1].LBA)*SectorSize:], p.Data)
	}
	return img, m, nil
}
