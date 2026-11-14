//go:build burrow

// bootparts.go finds the boot disk and every disk's volumes (W26-S05,
// W27-S02). The boot disk is the first disk, in the order below, whose
// sector 1 holds Burrow's manifest (the BIOS images) or whose GPT has an
// EFI system partition (the UEFI images, the USB stick), whatever its
// interface: hda on IDE, sda on AHCI (q35), vda, nvme0n1, a USB disk. Of
// the boot disk only its GPT's data partitions are volumes; any other disk
// is read by package part (GPT, a file system on the whole disk, or an
// MBR). An EFI system partition is never a volume (PIT-99). Docs:
// docs/04-plan/waves/WAVE-26-uefi.md (S05), WAVE-27-real-pc.md (S02),
// docs/06-guides/REAL_HARDWARE.md.
package main

import (
	"bytes"
	"sort"
	"strings"

	"burrow/kernel/block"
	"burrow/kernel/block/gpt"
	"burrow/kernel/block/part"
	"burrow/kernel/fs/ext2"
	"burrow/kernel/fs/fat"
)

// namedDisk is a volume and the name the log and the mount table use.
type namedDisk struct {
	name string
	dev  block.Device
}

// diskKinds orders the disks: IDE first (the old boot disk), then SATA,
// virtio, NVMe and USB, as the smokes and the run targets attach them.
var diskKinds = []string{"hd", "sd", "vd", "nvme", "ub"}

func diskRank(name string) int {
	for i, k := range diskKinds {
		if strings.HasPrefix(name, k) {
			return i
		}
	}
	return len(diskKinds)
}

// orderedDisks is every registered disk in diskKinds' order, then by name.
func orderedDisks() []string {
	names := block.Names()
	sort.SliceStable(names, func(i, j int) bool {
		ri, rj := diskRank(names[i]), diskRank(names[j])
		return ri < rj || ri == rj && names[i] < names[j]
	})
	return names
}

// isBootDisk: sector 1 is Burrow's manifest, or a GPT with an EFI system partition.
func isBootDisk(d block.Device) bool {
	sector := make([]byte, d.SectorSize())
	if d.ReadSectors(1, sector) != nil {
		return false
	}
	if bytes.HasPrefix(sector, []byte(manifestMagic)) {
		return true
	}
	tbl, err := gpt.Read(d)
	if err != nil {
		return false
	}
	_, ok := tbl.ESP()
	return ok
}

// bootDisk is the first boot disk; "" when there is none.
func bootDisk() (string, block.Device) {
	for _, n := range orderedDisks() {
		if d, _ := block.Get(n); d != nil && isBootDisk(d) {
			return n, d
		}
	}
	return "", nil
}

// hasFileSystem says whether a whole disk holds ext2 or FAT itself.
func hasFileSystem(d block.Device) bool {
	r := block.ReaderAt(d)
	if ext2.Probe(r) {
		return true
	}
	_, err := fat.Mount(r)
	return err == nil
}

// dataVolumes is every volume to mount: the boot disk's GPT data partitions
// first, then each other disk's volumes, in orderedDisks' order. A disk
// whose table cannot be read is reported through warn and skipped.
func dataVolumes(warn func(format string, a ...any)) []namedDisk {
	boot, _ := bootDisk()
	var out []namedDisk
	if boot != "" {
		d, _ := block.Get(boot)
		if tbl, err := gpt.Read(d); err == nil {
			for _, p := range tbl {
				if !p.IsESP() {
					out = append(out, namedDisk{part.Name(boot, p.Number), p.Device(d)})
				}
			}
		}
	}
	for _, n := range orderedDisks() {
		if n == boot {
			continue
		}
		d, _ := block.Get(n)
		l, err := part.Find(n, d, hasFileSystem)
		if err != nil {
			warn("%s: %v", n, err)
			continue
		}
		for _, v := range l.Volumes {
			out = append(out, namedDisk{v.Name, v.Dev})
		}
	}
	return out
}
