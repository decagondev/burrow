//go:build burrow

package main

import (
	"bytes"
	"fmt"
	"io"

	"burrow/kernel/block"
	"burrow/kernel/fs/fat"
	"burrow/kernel/klog"
)

// manifestMagic opens sector 1 of a Burrow boot disk (include/burrow/manifest.h).
const manifestMagic = "BRWMNFST"

// checkBootDisk reads the boot disk's manifest sector through the Go ATA
// driver: proof that the block layer reads what the loader read (W8-S06).
func checkBootDisk(log *klog.Logger) {
	d, ok := block.Get("hda")
	if !ok {
		log.Warnf("no hda")
		return
	}
	sector := make([]byte, d.SectorSize())
	if err := d.ReadSectors(1, sector); err != nil {
		log.Errorf("hda: %v", err)
		return
	}
	if !bytes.HasPrefix(sector, []byte(manifestMagic)) {
		log.Errorf("hda sector 1: no manifest (% x)", sector[:8])
		return
	}
	log.Infof("hda sector 1: boot manifest %q found", manifestMagic)
}

func listDisks(out io.Writer) {
	for _, n := range block.Names() {
		d, _ := block.Get(n)
		fmt.Fprintf(out, "%s %8d sectors of %d bytes (%d KiB)\n", n, d.Sectors(), d.SectorSize(),
			d.Sectors()*uint64(d.SectorSize())/1024)
	}
}

// mountData mounts the second disk at /mnt when it holds FAT32 (W8-S07).
func (k *kernel) mountData(log *klog.Logger) {
	d, ok := block.Get("hdb")
	if !ok {
		return
	}
	fsys, err := fat.Mount(block.ReaderAt(d))
	if err != nil {
		log.Warnf("hdb: %v", err)
		return
	}
	if err := k.fs.Mount("mnt", fsys, "fat32"); err != nil {
		log.Errorf("mount /mnt: %v", err)
		return
	}
	log.Infof("hdb: FAT32 volume %q mounted at /mnt", fsys.Label)
}
