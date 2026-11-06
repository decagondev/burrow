package virtioblk

import (
	"sync"
	"sync/atomic"
	"testing"

	"burrow/kernel/block"
	"burrow/kernel/block/blocktest"
	"burrow/kernel/virtio/virtiotest"
)

// fakeDisk is a virtio block device over an image in memory.
type fakeDisk struct {
	*virtiotest.Device
	mu      sync.Mutex // the image; serving holds it, one kick at a time
	image   []byte
	badLBA  int64 // a request touching it fails with IOERR (-1: none)
	flushes atomic.Int32
	disk    *Disk
}

func newFakeDisk(t *testing.T, sectors int, features uint64) *fakeDisk {
	t.Helper()
	f := &fakeDisk{Device: virtiotest.New(features, 128), image: blocktest.Pattern(sectors, sectorBytes), badLBA: -1}
	le.PutUint32(f.Config.B[cfgCapacity:], uint32(sectors))
	le.PutUint32(f.Config.B[cfgSegMax:], 126)
	le.PutUint32(f.Config.B[cfgBlkSize:], 512)
	f.OnKick = f.serve
	d, err := New(f.Virtio(), f.Mem.Page)
	if err != nil {
		t.Fatal(err)
	}
	f.disk = d
	return f
}

// serve answers every chain offered: header, data, status.
func (f *fakeDisk) serve(uint16) {
	f.mu.Lock()
	heads, chains := f.Take(0)
	for i, c := range chains {
		hdr := f.Mem.At(c[0].Addr, 16)
		typ, lba := le.Uint32(hdr), int64(le.Uint64(hdr[8:]))
		status, written := byte(statusOK), uint32(0)
		off := lba * sectorBytes
		for _, dsc := range c[1 : len(c)-1] {
			if f.badLBA >= lba && f.badLBA < lba+int64(dsc.Len)/sectorBytes || off+int64(dsc.Len) > int64(len(f.image)) {
				status = statusIOErr
				break
			}
			buf := f.Mem.At(dsc.Addr, int(dsc.Len))
			if typ == reqIn {
				copy(buf, f.image[off:])
				written += dsc.Len
			} else {
				copy(f.image[off:], buf)
			}
			off += int64(dsc.Len)
		}
		if typ == reqFlush {
			f.flushes.Add(1)
		}
		f.Mem.At(c[len(c)-1].Addr, 1)[0] = status
		f.Use(0, heads[i], written+1)
	}
	f.mu.Unlock()
	f.disk.Interrupt()
}

func TestConformance(t *testing.T) {
	f := newFakeDisk(t, 200, featFlush|featSegMax|featBlkSize)
	blocktest.Run(t, f.disk, blocktest.Pattern(200, sectorBytes))
	blocktest.RunWriter(t, f.disk)
	if f.flushes.Load() == 0 || !f.disk.CanFlush {
		t.Error("Flush sent no flush request")
	}
	big := make([]byte, 150*sectorBytes) // several requests in flight at once
	if err := f.disk.ReadSectors(10, big); err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	for i := range big {
		if big[i] != f.image[10*sectorBytes+i] {
			t.Fatalf("byte %d of a 150-sector read differs", i)
		}
	}
}

func TestManyReaders(t *testing.T) {
	f := newFakeDisk(t, 512, 0)
	var wg sync.WaitGroup
	for g := 0; g < 16; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			buf := make([]byte, 40*sectorBytes)
			for i := 0; i < 10; i++ {
				if err := f.disk.ReadSectors(uint64(g*20+i), buf); err != nil {
					t.Error(err)
					return
				}
			}
		}()
	}
	wg.Wait()
	if f.disk.CanFlush || f.disk.Flush() != nil || f.flushes.Load() != 0 {
		t.Error("a device without VIRTIO_BLK_F_FLUSH got a flush")
	}
}

func TestIOErrorAndReadOnly(t *testing.T) {
	f := newFakeDisk(t, 64, 0)
	f.badLBA = 30
	if err := f.disk.ReadSectors(28, make([]byte, 4*sectorBytes)); err == nil {
		t.Error("a failed request was not an error")
	}
	if err := f.disk.ReadSectors(0, make([]byte, 8*sectorBytes)); err != nil {
		t.Errorf("a good read after a failed one: %v", err)
	}
	ro := newFakeDisk(t, 64, featRO)
	if _, w := ro.disk.Device().(block.Writer); w || !ro.disk.ReadOnly {
		t.Error("a read-only device is a Writer")
	}
	if err := ro.disk.WriteSectors(0, make([]byte, sectorBytes)); err == nil {
		t.Error("a read-only device took a write")
	}
}
