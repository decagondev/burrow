// Package virtioblk drives virtio block devices (W23-S04): QEMU's
// virtio-blk-pci as a block.Writer (block.Device when the device is
// read-only). A request is a chain of descriptors: the 16-byte header, up
// to pagesMax data pages, the status byte the device writes. A few slots
// are in flight at once, each with its own descriptors and pages; the
// interrupt (on the line's goroutine) wakes each finished request. Never
// reuses a slot whose request the device has not returned (PIT-80); data
// pages are DMA pages, never the Go heap. Docs: WAVE-23-proc-virtio.md.
package virtioblk

import (
	"encoding/binary"
	"errors"
	"fmt"
	"sync"
	"time"

	"burrow/kernel/block"
	"burrow/kernel/virtio"
)

const (
	featSegMax  = 1 << 2 // VIRTIO_BLK_F_SEG_MAX
	featRO      = 1 << 5
	featBlkSize = 1 << 6
	featFlush   = 1 << 9

	cfgCapacity = 0  // u64, in 512-byte sectors
	cfgSegMax   = 12 // u32
	cfgBlkSize  = 20 // u32

	reqIn, reqOut, reqFlush = 0, 1, 4
	statusOK, statusIOErr   = 0, 1

	sectorBytes = 512
	pageBytes   = 4096
	pagesMax    = 8 // data pages per request: 32 KiB
	slotsMax    = 8
	queueMax    = 128 // the rings of 128 fit one page
	slotBytes   = 32  // a slot's header (16) and status byte in the header page
	waitMax     = 10 * time.Second
)

var le = binary.LittleEndian

// DMAPage hands out one zeroed DMA page (driver.Env.DMAPage).
type DMAPage func() (pa uint64, mem []byte, err error)

// Disk is a set-up virtio block device.
type Disk struct {
	dev       *virtio.Device
	q         *virtio.Queue
	mu        sync.Mutex // the queue
	free      chan *slot
	byHead    map[uint16]*slot
	sectors   uint64
	pages     int // data pages per request (seg_max may lower it)
	ReadOnly  bool
	CanFlush  bool
	BlockSize uint32
	Requests  uint64 // requests the device finished (the log, tests)
}

type slot struct {
	head   uint16
	hdr    []byte // header, then status at hdrBytes
	hdrPA  uint64
	data   [][]byte
	dataPA []uint64
	done   chan uint8
}

// New sets the device up: features, capacity, the request queue and its slots.
func New(dev *virtio.Device, dma DMAPage) (*Disk, error) {
	agreed, err := dev.Negotiate(featSegMax | featRO | featBlkSize | featFlush)
	if err != nil {
		return nil, err
	}
	d := &Disk{dev: dev, byHead: map[uint16]*slot{}, pages: pagesMax, BlockSize: sectorBytes,
		sectors:  uint64(dev.Config.R32(cfgCapacity)) | uint64(dev.Config.R32(cfgCapacity+4))<<32,
		ReadOnly: agreed&featRO != 0, CanFlush: agreed&featFlush != 0}
	if agreed&featSegMax != 0 {
		if m := int(dev.Config.R32(cfgSegMax)); m > 0 && m < d.pages {
			d.pages = m
		}
	}
	if agreed&featBlkSize != 0 {
		d.BlockSize = dev.Config.R32(cfgBlkSize)
	}
	size := min(dev.QueueMax(0), queueMax)
	slots := min(slotsMax, int(size)/(d.pages+2))
	if slots == 0 {
		return nil, errors.New("virtioblk: the request queue is too small")
	}
	qpa, qmem, err := dma()
	if err != nil {
		return nil, err
	}
	if d.q, err = virtio.NewQueue(0, size, qpa, qmem); err != nil {
		return nil, err
	}
	if err := d.makeSlots(slots, dma); err != nil {
		return nil, err
	}
	if err := dev.SetupQueue(d.q); err != nil {
		return nil, err
	}
	dev.Ready()
	return d, nil
}

// makeSlots gives each slot its descriptors, header and data pages.
func (d *Disk) makeSlots(n int, dma DMAPage) error {
	hpa, hmem, err := dma()
	if err != nil {
		return err
	}
	d.free = make(chan *slot, n)
	for i := 0; i < n; i++ {
		s := &slot{head: uint16(i * (d.pages + 2)), hdr: hmem[i*slotBytes : (i+1)*slotBytes],
			hdrPA: hpa + uint64(i*slotBytes), done: make(chan uint8, 1)}
		for k := 0; k < d.pages; k++ {
			pa, mem, err := dma()
			if err != nil {
				return err
			}
			s.data, s.dataPA = append(s.data, mem), append(s.dataPA, pa)
		}
		d.byHead[s.head] = s
		d.free <- s
	}
	return nil
}

// Interrupt takes back what the device finished; false: not its interrupt.
func (d *Disk) Interrupt() bool {
	if d.dev.ReadISR() == 0 {
		return false
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	for {
		head, _, ok := d.q.Used()
		if !ok {
			return true
		}
		if s := d.byHead[head]; s != nil {
			d.Requests++
			s.done <- s.hdr[16]
		}
	}
}

// do runs one request of at most d.pages pages: buf is read into (reqIn)
// or written from (reqOut); a flush has no data.
func (d *Disk) do(typ uint32, lba uint64, buf []byte) error {
	s := <-d.free
	le.PutUint32(s.hdr[0:], typ)
	le.PutUint32(s.hdr[4:], 0)
	le.PutUint64(s.hdr[8:], lba)
	s.hdr[16] = 0xFF
	flags := uint16(virtio.DescNext)
	if typ == reqIn {
		flags |= virtio.DescWrite
	}
	d.q.SetDescNext(s.head, s.hdrPA, 16, virtio.DescNext, s.head+1)
	next := s.head + 1
	for k := 0; k*pageBytes < len(buf); k++ {
		part := buf[k*pageBytes : min(len(buf), (k+1)*pageBytes)]
		if typ == reqOut {
			copy(s.data[k], part)
		}
		d.q.SetDescNext(next, s.dataPA[k], uint32(len(part)), flags, next+1)
		next++
	}
	d.q.SetDescNext(next, s.hdrPA+16, 1, virtio.DescWrite, 0)
	d.mu.Lock()
	d.q.Offer(s.head)
	d.dev.Kick(d.q)
	d.mu.Unlock()
	var st uint8
	select {
	case st = <-s.done:
	case <-time.After(waitMax):
		return fmt.Errorf("virtioblk: request at sector %d not answered in %v", lba, waitMax) // the slot stays lost
	}
	defer func() { d.free <- s }()
	if st != statusOK {
		return fmt.Errorf("virtioblk: sector %d: device status %d", lba, st)
	}
	if typ == reqIn {
		for k := 0; k*pageBytes < len(buf); k++ {
			copy(buf[k*pageBytes:min(len(buf), (k+1)*pageBytes)], s.data[k])
		}
	}
	return nil
}

func (d *Disk) SectorSize() int { return sectorBytes }
func (d *Disk) Sectors() uint64 { return d.sectors }

// ReadSectors reads buf from lba, in requests of d.pages pages in flight together.
func (d *Disk) ReadSectors(lba uint64, buf []byte) error {
	if err := block.CheckRange(d, lba, buf); err != nil {
		return err
	}
	return d.each(reqIn, lba, buf)
}

// WriteSectors writes buf at lba.
func (d *Disk) WriteSectors(lba uint64, buf []byte) error {
	if d.ReadOnly {
		return errors.New("virtioblk: the disk is read-only")
	}
	if err := block.CheckRange(d, lba, buf); err != nil {
		return err
	}
	return d.each(reqOut, lba, buf)
}

// Flush returns once the device has every earlier write on its medium
// (nothing to do for a device without a write cache to flush).
func (d *Disk) Flush() error {
	if !d.CanFlush {
		return nil
	}
	return d.do(reqFlush, 0, nil)
}

// each splits buf into requests and runs them at once, the first error wins.
func (d *Disk) each(typ uint32, lba uint64, buf []byte) error {
	chunk := d.pages * pageBytes
	var wg sync.WaitGroup
	errs := make([]error, (len(buf)+chunk-1)/chunk)
	for i := range errs {
		part := buf[i*chunk : min(len(buf), (i+1)*chunk)]
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs[i] = d.do(typ, lba+uint64(i*chunk/sectorBytes), part)
		}()
	}
	wg.Wait()
	return errors.Join(errs...)
}

// Device is d as the block layer should see it: a Writer unless read-only.
func (d *Disk) Device() block.Device {
	if d.ReadOnly {
		return readOnly{d}
	}
	return d
}

type readOnly struct{ d *Disk }

func (r readOnly) SectorSize() int                          { return sectorBytes }
func (r readOnly) Sectors() uint64                          { return r.d.sectors }
func (r readOnly) ReadSectors(lba uint64, buf []byte) error { return r.d.ReadSectors(lba, buf) }
