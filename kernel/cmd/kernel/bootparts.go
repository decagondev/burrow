//go:build burrow

// bootparts.go finds the data volumes on the boot disk itself (W26-S05):
// make uefi-usb puts the root and the data disks' volumes on one GPT disk
// beside the EFI system partition, so a single USB stick carries them all,
// mounted by label as separate disks are. The EFI system partition is
// never one of them (PIT-99), and a BIOS boot disk (a manifest, no GPT)
// has none. Docs: docs/04-plan/waves/WAVE-26-uefi.md (S05),
// docs/06-guides/REAL_HARDWARE.md.
package main

import (
	"fmt"

	"burrow/kernel/block"
	"burrow/kernel/block/gpt"
)

// namedDisk is a partition and the name the log and the mount table use.
type namedDisk struct {
	name string
	dev  block.Device
}

// bootDataPartitions are hda's GPT partitions other than the EFI system
// partition, named hda2.. by their entry numbers, as Linux names them.
func bootDataPartitions() []namedDisk {
	d, ok := block.Get("hda")
	if !ok {
		return nil
	}
	tbl, err := gpt.Read(d)
	if err != nil {
		return nil // a BIOS boot disk, or a GPT checkBootDisk already reported
	}
	var out []namedDisk
	for _, p := range tbl {
		if !p.IsESP() {
			out = append(out, namedDisk{fmt.Sprintf("hda%d", p.Number), p.Device(d)})
		}
	}
	return out
}
