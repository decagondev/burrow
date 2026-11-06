//go:build burrow

package main

// diskbench.go - make disk-bench's measurement (W23-S04): a
// "diskbench=<disk>" kernel argument writes 64 MiB to that disk from its
// start in 256 KiB calls, flushes, reads them back the same way and logs
// both speeds, so tests/smoke/disk-budgets.py can compare the IDE and
// virtio buses. Only for scratch disks: it destroys what is there. Never
// runs without the argument.

import (
	"bytes"
	"fmt"
	"os"
	"strings"
	"time"

	"burrow/kernel/block"
	"burrow/kernel/klog"
)

const (
	benchBytes = 64 << 20
	benchCall  = 256 << 10
)

func diskBench(log *klog.Logger) {
	for _, a := range os.Args[1:] {
		if name, ok := strings.CutPrefix(a, "diskbench="); ok {
			if err := benchDisk(log, name); err != nil {
				log.Errorf("%s: %v", name, err)
			}
		}
	}
}

func benchDisk(log *klog.Logger, name string) error {
	d, ok := block.Get(name)
	w, isW := d.(block.Writer)
	if !ok || !isW || d.Sectors()*uint64(d.SectorSize()) < benchBytes {
		return fmt.Errorf("not a writable disk of %d MiB or more", benchBytes>>20)
	}
	buf := bytes.Repeat([]byte("burrow disk-bench "), benchCall/18+1)[:benchCall]
	per := uint64(benchCall / d.SectorSize())
	start := time.Now()
	for lba := uint64(0); lba < benchBytes/uint64(d.SectorSize()); lba += per {
		if err := w.WriteSectors(lba, buf); err != nil {
			return err
		}
	}
	if err := w.Flush(); err != nil {
		return err
	}
	wrote := time.Since(start)
	start = time.Now()
	got := make([]byte, benchCall)
	for lba := uint64(0); lba < benchBytes/uint64(d.SectorSize()); lba += per {
		if err := d.ReadSectors(lba, got); err != nil {
			return err
		}
	}
	read := time.Since(start)
	if !bytes.Equal(got, buf) {
		return fmt.Errorf("read back something else than was written")
	}
	mibps := func(t time.Duration) float64 { return float64(benchBytes>>20) / t.Seconds() }
	log.Infof("%s: write %d MiB in %v (%.1f MiB/s), read in %v (%.1f MiB/s)", name, benchBytes>>20,
		wrote.Round(time.Millisecond), mibps(wrote), read.Round(time.Millisecond), mibps(read))
	return nil
}
