package virtio

import (
	"sync/atomic"
	"testing"
	"unsafe"
)

// regs is a fake configuration structure: plain registers, with hooks.
type regs struct {
	b       [256]byte
	onWrite func(off uint32)
	onRead  func(off uint32)
}

func (r *regs) R8(o uint32) uint8 {
	if r.onRead != nil {
		r.onRead(o)
	}
	return r.b[o]
}
func (r *regs) R16(o uint32) uint16 { return le.Uint16(r.b[o:]) }
func (r *regs) R32(o uint32) uint32 { return le.Uint32(r.b[o:]) }
func (r *regs) W8(o uint32, v uint8) {
	r.b[o] = v
	if r.onWrite != nil {
		r.onWrite(o)
	}
}
func (r *regs) W16(o uint32, v uint16) {
	le.PutUint16(r.b[o:], v)
	if r.onWrite != nil {
		r.onWrite(o)
	}
}
func (r *regs) W32(o uint32, v uint32) {
	le.PutUint32(r.b[o:], v)
	if r.onWrite != nil {
		r.onWrite(o)
	}
}

// fakeDev is a virtio device: its common configuration follows the spec's
// rules for features and status; queue 0 has room for 128.
type fakeDev struct {
	common, notify, isr regs
	offered             uint64
	refuse              bool
	driverFeat          uint64
	kicks               int
	queueAddr           [3]uint64
}

func newFakeDev(offered uint64) *fakeDev {
	f := &fakeDev{offered: offered}
	f.common.onWrite = func(o uint32) {
		c := &f.common
		switch o {
		case ccDeviceFeatureSel:
			le.PutUint32(c.b[ccDeviceFeature:], uint32(f.offered>>(32*le.Uint32(c.b[ccDeviceFeatureSel:]))))
		case ccDriverFeature:
			f.driverFeat |= uint64(le.Uint32(c.b[ccDriverFeature:])) << (32 * le.Uint32(c.b[ccDriverFeatureSel:]))
		case ccStatus:
			st := c.b[ccStatus]
			if st == 0 {
				f.driverFeat = 0
			}
			if st&StatusFeaturesOK != 0 && (f.refuse || f.driverFeat&^f.offered != 0) {
				c.b[ccStatus] = st &^ StatusFeaturesOK
			}
		case ccQueueSelect:
			if le.Uint16(c.b[ccQueueSelect:]) == 0 {
				le.PutUint16(c.b[ccQueueSize:], 128)
			} else {
				le.PutUint16(c.b[ccQueueSize:], 0)
			}
			le.PutUint16(c.b[ccQueueNotifyOff:], 3)
		case ccQueueDesc + 4, ccQueueDriver + 4, ccQueueDevice + 4:
			i := (o - ccQueueDesc) / 8
			f.queueAddr[i] = uint64(le.Uint32(c.b[o-4:])) | uint64(le.Uint32(c.b[o:]))<<32
		}
	}
	f.notify.onWrite = func(uint32) { f.kicks++ }
	f.isr.onRead = func(uint32) {}
	return f
}

func (f *fakeDev) device() *Device {
	return &Device{Common: &f.common, Notify: &f.notify, ISR: &f.isr, NotifyMultiplier: 4}
}

func TestNegotiateAgreesOnAskedFeaturesOnly(t *testing.T) {
	f := newFakeDev(FeatureVersion1 | 1<<0 | 1<<5)
	got, err := f.device().Negotiate(1 << 5)
	if err != nil || got != FeatureVersion1|1<<5 || f.driverFeat != got {
		t.Fatalf("agreed %#x (device saw %#x), %v", got, f.driverFeat, err)
	}
	if st := f.common.b[ccStatus]; st != StatusAcknowledge|StatusDriver|StatusFeaturesOK {
		t.Errorf("status %#x", st)
	}
}

// PIT-74: a legacy-only device (no VIRTIO_F_VERSION_1) is refused, loudly.
func TestNegotiateRefusesLegacyAndRefusal(t *testing.T) {
	f := newFakeDev(1 << 0)
	if _, err := f.device().Negotiate(0); err == nil || f.common.b[ccStatus]&StatusFailed == 0 {
		t.Errorf("legacy device: %v, status %#x", err, f.common.b[ccStatus])
	}
	f = newFakeDev(FeatureVersion1)
	f.refuse = true
	if _, err := f.device().Negotiate(0); err == nil {
		t.Error("a device that refuses FEATURES_OK was accepted")
	}
}

// The device's side of a queue: take what is available, write into each
// buffer (at its physical address in bufs), return it as used.
type fakeRing struct {
	q    *Queue
	bufs []byte // physical bufPA..
	seen uint16
}

const pagePA, bufPA = 0x10000, 0x20000

func (r *fakeRing) serve(t *testing.T, fill func(i int) byte) int {
	m := r.q.mem
	avail := uint16(atomic.LoadUint32((*uint32)(unsafe.Pointer(&m[r.q.availOff]))) >> 16)
	n := 0
	for ; r.seen != avail; r.seen++ {
		head := le.Uint16(m[r.q.availOff+ringHeader+2*int(r.seen%r.q.Size):])
		d := m[int(head)*descBytes:]
		addr, length, flags := le.Uint64(d), le.Uint32(d[8:]), le.Uint16(d[12:])
		if flags != DescWrite {
			t.Fatalf("descriptor %d flags %#x", head, flags)
		}
		b := r.bufs[addr-bufPA:][:length]
		for i := range b {
			b[i] = fill(int(head))
		}
		usedIdx := uint16(atomic.LoadUint32((*uint32)(unsafe.Pointer(&m[r.q.usedOff]))) >> 16)
		e := m[r.q.usedOff+ringHeader+usedElem*int(usedIdx%r.q.Size):]
		le.PutUint32(e, uint32(head))
		le.PutUint32(e[4:], length)
		atomic.StoreUint32((*uint32)(unsafe.Pointer(&m[r.q.usedOff])), uint32(usedIdx+1)<<16)
		n++
	}
	return n
}

// W22-S04: a 64-entry queue set up on the device, its buffers offered,
// filled and taken back many times round the ring and across the 16-bit
// index's wrap, each buffer's contents and length as the device wrote them.
func TestQueueRoundTripsAcrossTheWrap(t *testing.T) {
	f := newFakeDev(FeatureVersion1)
	d := f.device()
	if _, err := d.Negotiate(0); err != nil {
		t.Fatal(err)
	}
	q, err := NewQueue(0, 64, pagePA, make([]byte, 4096))
	if err != nil {
		t.Fatal(err)
	}
	if err := d.SetupQueue(q); err != nil {
		t.Fatal(err)
	}
	if f.queueAddr != [3]uint64{pagePA, pagePA + 64*16, q.UsedPA()} || q.UsedPA()%4 != 0 || f.common.b[ccQueueEnable] != 1 {
		t.Fatalf("queue addresses %#x, used at %#x", f.queueAddr, q.UsedPA())
	}
	q.availIdx, q.lastUsed = 65500, 65500 // so the run crosses 65535 -> 0
	atomic.StoreUint32(q.word(q.usedOff), 65500<<16)
	ring := &fakeRing{q: q, bufs: make([]byte, 64*8), seen: 65500}
	for i := uint16(0); i < 64; i++ {
		q.SetDesc(i, bufPA+8*uint64(i), 8, DescWrite)
		q.Offer(i)
	}
	d.Kick(q)
	if f.kicks != 1 || le.Uint16(f.notify.b[3*4:]) != 0 {
		t.Errorf("kick: %d, at offset notify_off * multiplier", f.kicks)
	}
	total := 0
	for round := 0; round < 5; round++ {
		if n := ring.serve(t, func(i int) byte { return byte(i + round) }); n != 64 {
			t.Fatalf("round %d: device served %d", round, n)
		}
		for k := 0; k < 64; k++ {
			id, n, ok := q.Used()
			if !ok || n != 8 || ring.bufs[8*int(id)] != byte(int(id)+round) {
				t.Fatalf("round %d: used %d %d %v", round, id, n, ok)
			}
			q.Offer(id)
			total++
		}
		if _, _, ok := q.Used(); ok {
			t.Fatal("a used entry the device never wrote")
		}
	}
	if total != 320 || q.lastUsed > 65500 { // wrapped
		t.Errorf("took %d, index %d", total, q.lastUsed)
	}
}

func TestFindCaps(t *testing.T) {
	var cs [256]byte
	le.PutUint32(cs[pciStatus:], 1<<20)
	cs[pciCapPtr] = 0x40
	put := func(at, next, typ, bar uint8, off, length uint32) {
		cs[at], cs[at+1], cs[at+3], cs[at+4] = capVendor, next, typ, bar
		le.PutUint32(cs[at+8:], off)
		le.PutUint32(cs[at+12:], length)
	}
	put(0x40, 0x50, cfgCommon, 4, 0x0000, 0x38)
	cs[0x50], cs[0x51] = 0x05, 0x60 // MSI: not ours, skipped
	put(0x60, 0x74, cfgNotify, 4, 0x3000, 0x1000)
	le.PutUint32(cs[0x60+16:], 4)
	put(0x74, 0x84, cfgISR, 4, 0x1000, 0x1000)
	put(0x84, 0, cfgDevice, 4, 0x2000, 0x1000)
	read := func(off uint8) uint32 { return le.Uint32(cs[off:]) }
	c, err := FindCaps(read)
	if err != nil {
		t.Fatal(err)
	}
	if c.Common != (Region{4, 0, 0x38}) || c.Notify.Offset != 0x3000 || c.NotifyMultiplier != 4 ||
		c.ISR.Offset != 0x1000 || c.Device.Offset != 0x2000 {
		t.Errorf("caps %+v", c)
	}
	cs[0x75] = 0 // the list ends before the device structure
	if _, err := FindCaps(read); err == nil {
		t.Error("a list without a device structure was accepted")
	}
}
