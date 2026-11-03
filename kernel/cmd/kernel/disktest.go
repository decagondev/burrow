//go:build burrow

package main

// disktest.go - the on-target check of disk writes (W13-S03). A
// "disktest=<disk>" kernel argument makes the kernel overwrite the last
// diskTestSectors sectors of that disk with a pattern, flush, and read them
// back; tests/smoke/checkdisk.py then checks the disk image on the host, so
// the bytes are known to have reached the disk. Only for scratch disks: it
// destroys what is there. Never runs without the argument.

import (
	"bytes"
	"fmt"
	"os"
	"strings"

	"burrow/kernel/block"
	"burrow/kernel/klog"
)

const diskTestSectors = 64

// diskTestSector is the pattern for sector lba (checkdisk.py computes the same).
func diskTestSector(lba uint64, size int) []byte {
	b := make([]byte, size)
	for i := range b {
		b[i] = byte(uint64(i)*13 + lba)
	}
	copy(b, fmt.Sprintf("burrow disktest lba %d\n", lba))
	return b
}

func diskTest(log *klog.Logger) {
	for _, a := range os.Args[1:] {
		name, ok := strings.CutPrefix(a, "disktest=")
		if !ok {
			continue
		}
		if err := writeAndCheck(name); err != nil {
			log.Errorf("%s: %v", name, err)
			continue
		}
		log.Infof("%s: wrote %d sectors, flushed, read them back", name, diskTestSectors)
	}
}

func writeAndCheck(name string) error {
	d, ok := block.Get(name)
	if !ok {
		return fmt.Errorf("no such disk")
	}
	w, ok := d.(block.Writer)
	if !ok || d.Sectors() < diskTestSectors {
		return fmt.Errorf("not a writable disk of %d sectors or more", diskTestSectors)
	}
	first, ss := d.Sectors()-diskTestSectors, d.SectorSize()
	var want []byte
	for lba := first; lba < d.Sectors(); lba++ {
		want = append(want, diskTestSector(lba, ss)...)
	}
	if err := w.WriteSectors(first, want); err != nil {
		return err
	}
	if err := w.Flush(); err != nil {
		return err
	}
	got := make([]byte, len(want))
	if err := d.ReadSectors(first, got); err != nil {
		return err
	}
	if !bytes.Equal(got, want) {
		return fmt.Errorf("read back something else than was written")
	}
	return nil
}
