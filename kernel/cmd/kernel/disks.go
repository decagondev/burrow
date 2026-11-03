//go:build burrow

package main

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"io/fs"
	"strings"

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
// /home instead of /mnt. make run-dev attaches such a disk. qtLabel marks
// the Qt data disk (W15-S02): it mounts at /opt/qt, and its qt.env joins
// the environment of every program the console runs.
const (
	homeLabel = "BURROWHOME"
	qtLabel   = "QTDATA"
	qtEnvFile = "opt/qt/qt.env"
)

// mountData mounts every data disk (hdb, and since W15-S06 the secondary
// channel's hdc and hdd) that holds FAT32 (W8-S07), read-write when the
// disk can be written (W13-S04), at the place its label names: /home for
// the home disk (W13-S05), /opt/qt for the Qt disk (W15-S02), /mnt for the
// first other one.
func (k *kernel) mountData(log *klog.Logger) {
	for _, name := range []string{"hdb", "hdc", "hdd"} {
		if d, ok := block.Get(name); ok {
			k.mountDisk(log, name, d)
		}
	}
}

func (k *kernel) mountDisk(log *klog.Logger, name string, d block.Device) {
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
		log.Warnf("%s: %v", name, err)
		return
	}
	point := "mnt"
	switch fsys.Label {
	case homeLabel:
		point = "home"
	case qtLabel:
		point = "opt/qt"
	}
	if err := k.fs.Mount(point, fsys, "fat32"); err != nil {
		log.Errorf("%s: mount /%s: %v", name, point, err)
		return
	}
	log.Infof("%s: FAT32 volume %q mounted at /%s (%s)", name, fsys.Label, point, mode)
	if point == "opt/qt" {
		k.env = readEnv(k.fs, qtEnvFile)
		log.Infof("%s: %d variables for every program", qtEnvFile, len(k.env))
	}
}

// readEnv reads KEY=VALUE lines (blank lines and # comments skipped).
func readEnv(fsys fs.FS, name string) []string {
	data, err := fs.ReadFile(fsys, name)
	if err != nil {
		return nil
	}
	var env []string
	sc := bufio.NewScanner(bytes.NewReader(data))
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line != "" && !strings.HasPrefix(line, "#") && strings.Contains(line, "=") {
			env = append(env, line)
		}
	}
	return env
}

// fatDisk is a block.Writer as the fat.Disk a writable mount needs.
type fatDisk struct {
	io.ReaderAt
	io.WriterAt
	w block.Writer
}

func (d fatDisk) Flush() error { return d.w.Flush() }
