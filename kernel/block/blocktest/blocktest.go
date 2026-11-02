// Package blocktest is the conformance test every block.Device must pass.
package blocktest

import (
	"bytes"
	"errors"
	"io"
	"testing"

	"burrow/kernel/block"
)

// Run checks d against want, its expected contents (a whole number of
// sectors): single and multi-sector reads, the last sector, reads past the
// end, bad buffer sizes, and ReaderAt at unaligned offsets.
func Run(t *testing.T, d block.Device, want []byte) {
	t.Helper()
	ss := d.SectorSize()
	if ss <= 0 || len(want)%ss != 0 || d.Sectors() != uint64(len(want)/ss) {
		t.Fatalf("geometry: sector %d, %d sectors, want %d bytes", ss, d.Sectors(), len(want))
	}
	n := d.Sectors()
	for _, c := range []struct{ lba, count uint64 }{{0, 1}, {1, 3}, {n - 1, 1}, {0, n}} {
		buf := make([]byte, c.count*uint64(ss))
		if err := d.ReadSectors(c.lba, buf); err != nil {
			t.Fatalf("ReadSectors(%d, %d sectors): %v", c.lba, c.count, err)
		}
		if w := want[c.lba*uint64(ss):][:len(buf)]; !bytes.Equal(buf, w) {
			t.Fatalf("ReadSectors(%d, %d sectors) returned the wrong bytes", c.lba, c.count)
		}
	}
	for _, c := range []struct{ lba, count uint64 }{{n, 1}, {n - 1, 2}, {1 << 40, 1}} {
		if err := d.ReadSectors(c.lba, make([]byte, c.count*uint64(ss))); !errors.Is(err, block.ErrRange) {
			t.Errorf("ReadSectors(%d, %d sectors) past the end = %v, want ErrRange", c.lba, c.count, err)
		}
	}
	if err := d.ReadSectors(0, make([]byte, ss+1)); err == nil {
		t.Error("ReadSectors accepted a buffer that is not whole sectors")
	}
	r := block.ReaderAt(d)
	p := make([]byte, ss+7)
	if k, err := r.ReadAt(p, int64(ss)-3); err != nil || k != len(p) || !bytes.Equal(p, want[ss-3:2*ss+4]) {
		t.Errorf("ReadAt across sectors: %d, %v", k, err)
	}
	if k, err := r.ReadAt(p, int64(len(want))-5); k != 5 || err != io.EOF {
		t.Errorf("ReadAt at the end = %d, %v; want 5, EOF", k, err)
	}
}

// Pattern is test contents where every sector differs.
func Pattern(sectors, sectorSize int) []byte {
	b := make([]byte, sectors*sectorSize)
	for i := range b {
		b[i] = byte(i*7 + i/sectorSize)
	}
	return b
}
