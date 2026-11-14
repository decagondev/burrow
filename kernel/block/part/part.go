// Package part finds the volumes on a disk (W27-S02): a GPT's partitions
// (its EFI system partition named apart, never a volume: PIT-99), an MBR's
// primary partitions, or the whole disk when it holds a file system of its
// own. Every block device the kernel finds goes through Find, whatever its
// driver, so a volume is mounted by its label wherever it is. Never reads
// past a disk's end and never trusts an entry it has not checked; logical
// partitions inside an extended one are not read.
// Docs: docs/04-plan/waves/WAVE-27-real-pc.md (S02).
package part

import (
	"encoding/binary"
	"errors"
	"fmt"

	"burrow/kernel/block"
	"burrow/kernel/block/gpt"
)

// Kind is what Find found on a disk.
type Kind string

const (
	GPT   Kind = "GPT"
	MBR   Kind = "MBR"
	Whole Kind = "whole disk"
)

const (
	mbrSigOffset   = 510
	mbrEntries     = 446
	mbrEntryBytes  = 16
	mbrStatusBoot  = 0x80
	typeProtective = 0xEE
	typeESP        = 0xEF // an EFI system partition on an MBR disk: never a volume (PIT-99)
)

var extended = map[byte]bool{0x05: true, 0x0F: true, 0x85: true}

// Volume is one partition (or the whole disk) as a disk of its own.
type Volume struct {
	Name   string // "sda1", "nvme0n1p2", or the disk's own name
	Number int    // the partition's number, 0 for a whole disk
	Dev    block.Device
}

// Layout is a disk's volumes and, on a GPT disk, its EFI system partition.
type Layout struct {
	Kind    Kind
	Volumes []Volume
	ESP     *gpt.Partition
}

// Name is partition n of disk, as Linux names it: a disk whose name ends in
// a digit takes a "p" ("nvme0n1p1"), the others none ("sda1").
func Name(disk string, n int) string {
	if c := disk[len(disk)-1]; c >= '0' && c <= '9' {
		return fmt.Sprintf("%sp%d", disk, n)
	}
	return fmt.Sprintf("%s%d", disk, n)
}

// Find reads d's layout. hasFS says whether a whole disk holds a file system
// (the caller's probes: ext2, FAT); one that does is a single volume even
// though a FAT boot sector carries an MBR's signature.
func Find(disk string, d block.Device, hasFS func(block.Device) bool) (Layout, error) {
	tbl, err := gpt.Read(d)
	switch {
	case err == nil:
		l := Layout{Kind: GPT}
		for _, p := range tbl {
			if p.IsESP() && l.ESP == nil {
				esp := p
				l.ESP = &esp
				continue
			}
			l.Volumes = append(l.Volumes, Volume{Name(disk, p.Number), p.Number, p.Device(d)})
		}
		return l, nil
	case !errors.Is(err, gpt.ErrNotGPT):
		return Layout{}, err
	}
	whole := Layout{Kind: Whole, Volumes: []Volume{{Name: disk, Dev: d}}}
	if hasFS != nil && hasFS(d) {
		return whole, nil
	}
	if vols, ok := readMBR(disk, d); ok {
		return Layout{Kind: MBR, Volumes: vols}, nil
	}
	return whole, nil
}

// readMBR is d's primary partitions; false when sector 0 is not an MBR
// (no signature, a status that is neither 0 nor 0x80, an entry past the
// end) or holds only a GPT's protective entry.
func readMBR(disk string, d block.Device) ([]Volume, bool) {
	ss := d.SectorSize()
	if ss < 512 || d.Sectors() == 0 {
		return nil, false
	}
	s0 := make([]byte, ss)
	if d.ReadSectors(0, s0) != nil || s0[mbrSigOffset] != 0x55 || s0[mbrSigOffset+1] != 0xAA {
		return nil, false
	}
	var vols []Volume
	for i := 0; i < 4; i++ {
		e := s0[mbrEntries+i*mbrEntryBytes:][:mbrEntryBytes]
		status, typ := e[0], e[4]
		first := uint64(binary.LittleEndian.Uint32(e[8:]))
		count := uint64(binary.LittleEndian.Uint32(e[12:]))
		if status != 0 && status != mbrStatusBoot {
			return nil, false
		}
		if typ == 0 || count == 0 {
			continue
		}
		if typ == typeProtective || first == 0 || first+count > d.Sectors() {
			return nil, false
		}
		if extended[typ] || typ == typeESP {
			continue
		}
		p := gpt.Partition{Number: i + 1, First: first, Last: first + count - 1}
		vols = append(vols, Volume{Name(disk, i+1), i + 1, p.Device(d)})
	}
	return vols, len(vols) > 0
}
