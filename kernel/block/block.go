// Package block is the kernel's block layer (E07.F05): a Device reads whole
// sectors; ReaderAt turns one into an io.ReaderAt for file systems; a small
// registry names the disks drivers find ("hda", "hdb"). Every Device passes
// the same conformance test (block/blocktest): the RAM disk and the ATA
// driver alike (Liskov).
package block

import (
	"errors"
	"fmt"
	"io"
	"sort"
	"sync"
)

// Device is a disk. ReadSectors fills buf (a whole number of sectors)
// starting at sector lba, or fails without a partial read past the end.
type Device interface {
	SectorSize() int
	Sectors() uint64
	ReadSectors(lba uint64, buf []byte) error
}

// ErrRange is a read past the end of the device.
var ErrRange = errors.New("block: read past the end of the device")

// CheckRange validates a ReadSectors call; drivers call it first.
func CheckRange(d Device, lba uint64, buf []byte) error {
	ss := d.SectorSize()
	if len(buf)%ss != 0 {
		return fmt.Errorf("block: buffer of %d bytes is not whole %d-byte sectors", len(buf), ss)
	}
	if n := uint64(len(buf) / ss); lba > d.Sectors() || n > d.Sectors()-lba {
		return ErrRange
	}
	return nil
}

// RAM is a disk in memory.
type RAM struct {
	data []byte
	ss   int
}

// NewRAM uses data as the disk's contents (len a multiple of sectorSize).
func NewRAM(data []byte, sectorSize int) *RAM { return &RAM{data: data, ss: sectorSize} }

func (r *RAM) SectorSize() int { return r.ss }
func (r *RAM) Sectors() uint64 { return uint64(len(r.data) / r.ss) }

func (r *RAM) ReadSectors(lba uint64, buf []byte) error {
	if err := CheckRange(r, lba, buf); err != nil {
		return err
	}
	copy(buf, r.data[lba*uint64(r.ss):])
	return nil
}

// ReaderAt reads any byte range of d by reading the sectors around it.
func ReaderAt(d Device) io.ReaderAt { return readerAt{d} }

type readerAt struct{ d Device }

func (r readerAt) ReadAt(p []byte, off int64) (int, error) {
	ss := int64(r.d.SectorSize())
	size := int64(r.d.Sectors()) * ss
	if off < 0 {
		return 0, errors.New("block: negative offset")
	}
	if off >= size {
		return 0, io.EOF
	}
	want := min(int64(len(p)), size-off)
	first, last := off/ss, (off+want+ss-1)/ss
	buf := make([]byte, (last-first)*ss)
	if err := r.d.ReadSectors(uint64(first), buf); err != nil {
		return 0, err
	}
	n := copy(p, buf[off-first*ss:off-first*ss+want])
	if n < len(p) {
		return n, io.EOF
	}
	return n, nil
}

var (
	mu    sync.Mutex
	disks = map[string]Device{}
)

// Register names a disk ("hda"); drivers call it.
func Register(name string, d Device) {
	mu.Lock()
	defer mu.Unlock()
	disks[name] = d
}

// Get returns a registered disk.
func Get(name string) (Device, bool) {
	mu.Lock()
	defer mu.Unlock()
	d, ok := disks[name]
	return d, ok
}

// Names lists the registered disks in order.
func Names() []string {
	mu.Lock()
	defer mu.Unlock()
	out := make([]string, 0, len(disks))
	for n := range disks {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}
