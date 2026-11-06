// Package virtiotest is a fake virtio 1.0 device for drivers' host tests
// (W23-S04: virtio-blk, then virtio-net): its common configuration follows
// the spec's rules for features, status and queues; DMA pages live in a
// Memory the device reads and writes by physical address; the device's side
// of each queue takes the chains offered and returns them as used. Never
// used outside tests. Docs: docs/04-plan/waves/WAVE-23-proc-virtio.md.
package virtiotest

import (
	"encoding/binary"
	"fmt"
	"sync"
	"sync/atomic"
	"unsafe"

	"burrow/kernel/virtio"
)

var le = binary.LittleEndian

// the common configuration's registers (virtio 1.0, 4.1.4.3)
const (
	ccDeviceFeatureSel = 0x00
	ccDeviceFeature    = 0x04
	ccDriverFeatureSel = 0x08
	ccDriverFeature    = 0x0C
	ccStatus           = 0x14
	ccQueueSelect      = 0x16
	ccQueueSize        = 0x18
	ccQueueNotifyOff   = 0x1E
	ccQueueDesc        = 0x20
	ccQueueDevice      = 0x30
	pageBytes          = 4096
	descBytes          = 16
)

// Regs is a fake configuration structure: plain registers with hooks,
// safe for the driver's goroutines and the device's.
type Regs struct {
	mu          sync.Mutex
	B           [256]byte
	OnWrite     func(off uint32) // called with the lock held
	ClearOnRead bool             // the ISR: a read returns and clears it
}

func (r *Regs) R8(o uint32) uint8 {
	r.mu.Lock()
	defer r.mu.Unlock()
	v := r.B[o]
	if r.ClearOnRead {
		r.B[o] = 0
	}
	return v
}
func (r *Regs) R16(o uint32) uint16 { r.mu.Lock(); defer r.mu.Unlock(); return le.Uint16(r.B[o:]) }
func (r *Regs) R32(o uint32) uint32 { r.mu.Lock(); defer r.mu.Unlock(); return le.Uint32(r.B[o:]) }
func (r *Regs) W8(o uint32, v uint8) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.B[o] = v
	r.hook(o)
}
func (r *Regs) W16(o uint32, v uint16) {
	r.mu.Lock()
	defer r.mu.Unlock()
	le.PutUint16(r.B[o:], v)
	r.hook(o)
}
func (r *Regs) W32(o uint32, v uint32) {
	r.mu.Lock()
	defer r.mu.Unlock()
	le.PutUint32(r.B[o:], v)
	r.hook(o)
}
func (r *Regs) hook(o uint32) {
	if r.OnWrite != nil {
		r.OnWrite(o)
	}
}

// Memory is the DMA pages the driver was given, by physical address.
type Memory struct {
	mu    sync.Mutex
	pages map[uint64][]byte
	next  uint64
}

// Page hands out a zeroed page (driver.Env.DMAPage's shape).
func (m *Memory) Page() (uint64, []byte, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.pages == nil {
		m.pages, m.next = map[uint64][]byte{}, 0x100000
	}
	pa, b := m.next, make([]byte, pageBytes)
	m.pages[pa] = b
	m.next += pageBytes
	return pa, b, nil
}

// At is n bytes at physical address pa, inside one page.
func (m *Memory) At(pa uint64, n int) []byte {
	m.mu.Lock()
	defer m.mu.Unlock()
	page := m.pages[pa&^(pageBytes-1)]
	off := int(pa & (pageBytes - 1))
	if page == nil || off+n > pageBytes {
		panic(fmt.Sprintf("virtiotest: %d bytes at %#x are not one DMA page", n, pa))
	}
	return page[off : off+n]
}

// Device is a fake virtio device with queues of QueueSize.
type Device struct {
	Common, Notify, ISR, Config Regs
	Mem                         Memory
	Offered, Agreed             uint64
	QueueSize                   uint16
	OnKick                      func(queue uint16) // in its own goroutine

	mu     sync.Mutex
	queues map[uint16]*queue
	sel    uint16
	rings  sync.Mutex // the device's side of the queues: one engine at a time
}

type queue struct {
	size               uint16
	desc, avail, used  uint64
	lastAvail, usedIdx uint16
}

// New is a device offering features (VIRTIO_F_VERSION_1 added).
func New(features uint64, queueSize uint16) *Device {
	d := &Device{Offered: features | virtio.FeatureVersion1, QueueSize: queueSize, queues: map[uint16]*queue{}}
	d.ISR.ClearOnRead = true
	d.Common.OnWrite = d.common
	d.Notify.OnWrite = func(o uint32) {
		q := uint16(le.Uint16(d.Notify.B[o:]))
		if d.OnKick != nil {
			go d.OnKick(q)
		}
	}
	return d
}

// common follows the driver's writes (Common's lock held).
func (d *Device) common(o uint32) {
	c := &d.Common.B
	d.mu.Lock()
	defer d.mu.Unlock()
	switch o {
	case ccDeviceFeatureSel:
		le.PutUint32(c[ccDeviceFeature:], uint32(d.Offered>>(32*le.Uint32(c[ccDeviceFeatureSel:]))))
	case ccDriverFeature:
		d.Agreed |= uint64(le.Uint32(c[ccDriverFeature:])) << (32 * le.Uint32(c[ccDriverFeatureSel:]))
	case ccStatus:
		if c[ccStatus] == 0 {
			d.Agreed = 0
		}
		if c[ccStatus]&virtio.StatusFeaturesOK != 0 && d.Agreed&^d.Offered != 0 {
			c[ccStatus] &^= virtio.StatusFeaturesOK
		}
	case ccQueueSelect:
		d.sel = le.Uint16(c[ccQueueSelect:])
		le.PutUint16(c[ccQueueSize:], d.QueueSize)
		le.PutUint16(c[ccQueueNotifyOff:], d.sel)
	case ccQueueSize:
		d.q().size = le.Uint16(c[ccQueueSize:])
	case ccQueueDesc + 4, ccQueueDesc + 12, ccQueueDevice + 4:
		addr := uint64(le.Uint32(c[o-4:])) | uint64(le.Uint32(c[o:]))<<32
		switch o {
		case ccQueueDesc + 4:
			d.q().desc = addr
		case ccQueueDesc + 12:
			d.q().avail = addr
		default:
			d.q().used = addr
		}
	}
}

func (d *Device) q() *queue {
	if d.queues[d.sel] == nil {
		d.queues[d.sel] = &queue{size: d.QueueSize}
	}
	return d.queues[d.sel]
}

// Desc is one descriptor of a chain.
type Desc struct {
	Addr  uint64
	Len   uint32
	Write bool // the device writes it
}

// Take is the chains offered on queue qi since the last Take, each as its
// head and descriptors.
func (d *Device) Take(qi uint16) (heads []uint16, chains [][]Desc) {
	d.rings.Lock()
	defer d.rings.Unlock()
	d.mu.Lock()
	q := d.queues[qi]
	d.mu.Unlock()
	if q == nil || q.avail == 0 {
		return nil, nil
	}
	idx := uint16(atomic.LoadUint32(word(d.Mem.At(q.avail, 4))) >> 16) // as the driver stores it
	for ; q.lastAvail != idx; q.lastAvail++ {
		head := le.Uint16(d.Mem.At(q.avail+4+2*uint64(q.lastAvail%q.size), 2))
		var chain []Desc
		for i, n := head, 0; n < int(q.size); n++ {
			b := d.Mem.At(q.desc+uint64(i)*descBytes, descBytes)
			flags := le.Uint16(b[12:])
			chain = append(chain, Desc{Addr: le.Uint64(b), Len: le.Uint32(b[8:]), Write: flags&virtio.DescWrite != 0})
			if flags&virtio.DescNext == 0 {
				break
			}
			i = le.Uint16(b[14:])
		}
		heads, chains = append(heads, head), append(chains, chain)
	}
	return heads, chains
}

// Use returns head on queue qi as used, written bytes n, and raises the
// queue interrupt in the ISR.
func (d *Device) Use(qi, head uint16, n uint32) {
	d.rings.Lock()
	defer d.rings.Unlock()
	d.mu.Lock()
	q := d.queues[qi]
	d.mu.Unlock()
	e := d.Mem.At(q.used+4+8*uint64(q.usedIdx%q.size), 8)
	le.PutUint32(e, uint32(head))
	le.PutUint32(e[4:], n)
	q.usedIdx++
	atomic.StoreUint32(word(d.Mem.At(q.used, 4)), uint32(q.usedIdx)<<16) // after the entry, as a device must
	d.ISR.mu.Lock()
	d.ISR.B[0] |= virtio.ISRQueue
	d.ISR.mu.Unlock()
}

// word is the ring header (flags, idx) the driver reads and writes whole.
func word(b []byte) *uint32 { return (*uint32)(unsafe.Pointer(&b[0])) }

// Virtio is the driver's view of the device.
func (d *Device) Virtio() *virtio.Device {
	return &virtio.Device{Common: &d.Common, Notify: &d.Notify, ISR: &d.ISR, Config: &d.Config, NotifyMultiplier: 2}
}
