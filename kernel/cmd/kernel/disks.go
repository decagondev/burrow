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

// homeLabel marks a FAT volume as the user's home (W13-S05): it mounts at
// /home instead of /mnt. make run-dev attaches such a disk.
const homeLabel = "BURROWHOME"

// mountData mounts the second disk at /mnt when it holds FAT32 (W8-S07),
// read-write when the disk can be written (W13-S04), at /home when its
// label says it is the home disk (W13-S05).
func (k *kernel) mountData(log *klog.Logger) {
	d, ok := block.Get("hdb")
	if !ok {
		return
	}
	var fsys *fat.FS
	var err error
	mode := "read-only"
	if w, isW := d.(block.Writer); isW {
		fsys, err = fat.MountRW(fatDisk{block.ReaderAt(d), block.WriterAt(w), w})
		mode = "read-write"
	} else {
		fsys, err = fat.Mount(block.ReaderAt(d))
	}
	if err != nil {
		log.Warnf("hdb: %v", err)
		return
	}
	point := "mnt"
	if fsys.Label == homeLabel {
		point = "home"
	}
	if err := k.fs.Mount(point, fsys, "fat32"); err != nil {
		log.Errorf("mount /%s: %v", point, err)
		return
	}
	log.Infof("hdb: FAT32 volume %q mounted at /%s (%s)", fsys.Label, point, mode)
}

// fatDisk is a block.Writer as the fat.Disk a writable mount needs.
type fatDisk struct {
	io.ReaderAt
	io.WriterAt
	w block.Writer
}

func (d fatDisk) Flush() error { return d.w.Flush() }
