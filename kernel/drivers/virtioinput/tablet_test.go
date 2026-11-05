package virtioinput

import (
	"encoding/binary"
	"testing"

	"burrow/kernel/virtio"
)

var le = binary.LittleEndian

// regs is plain registers with a write hook.
type regs struct {
	b       [256]byte
	onWrite func(off uint32)
	onRead  func(off uint32) // before the read
}

func (r *regs) R8(o uint32) uint8 {
	if r.onRead != nil {
		r.onRead(o)
	}
	return r.b[o]
}
func (r *regs) R16(o uint32) uint16 { return le.Uint16(r.b[o:]) }
func (r *regs) R32(o uint32) uint32 { return le.Uint32(r.b[o:]) }
func (r *regs) hook(o uint32) {
	if r.onWrite != nil {
		r.onWrite(o)
	}
}
func (r *regs) W8(o uint32, v uint8)   { r.b[o] = v; r.hook(o) }
func (r *regs) W16(o uint32, v uint16) { le.PutUint16(r.b[o:], v); r.hook(o) }
func (r *regs) W32(o uint32, v uint32) { le.PutUint32(r.b[o:], v); r.hook(o) }

// fakeTablet is QEMU's virtio tablet as far as the driver sees it.
type fakeTablet struct {
	common, notify, isr, cfg regs
	pages                    map[uint64][]byte
	next                     uint64
	desc, used               uint64
	usedIdx, availSeen       uint16
}

func newFakeTablet() *fakeTablet {
	f := &fakeTablet{pages: map[uint64][]byte{}, next: 0x100000}
	f.common.onWrite = func(o uint32) {
		c := &f.common
		switch o {
		case 0x00: // device feature select: VERSION_1 in the high word
			le.PutUint32(c.b[0x04:], le.Uint32(c.b[0x00:]))
		case 0x16:
			le.PutUint16(c.b[0x18:], 64)
		case 0x24:
			f.desc = uint64(le.Uint32(c.b[0x20:]))
		case 0x34:
			f.used = uint64(le.Uint32(c.b[0x30:]))
		}
	}
	f.cfg.onWrite = func(o uint32) {
		c := &f.cfg
		clear(c.b[2:])
		switch {
		case c.b[0] == cfgIDName:
			n := copy(c.b[cfgUnion:], "QEMU Virtio Tablet\x00") // QEMU counts the NUL
			c.b[cfgSize] = uint8(n)
		case c.b[0] == cfgAbsInfo && c.b[1] <= absY:
			le.PutUint32(c.b[cfgUnion+4:], 32767)
			c.b[cfgSize] = 20
		}
	}
	f.isr.onRead = func(uint32) {}
	return f
}

func (f *fakeTablet) dma() (uint64, []byte, error) {
	pa := f.next
	f.next += 4096
	f.pages[pa] = make([]byte, 4096)
	return pa, f.pages[pa], nil
}

func (f *fakeTablet) at(pa uint64) []byte {
	page := pa &^ 4095
	return f.pages[page][pa-page:]
}

// send fills the next offered buffers with events and raises the ISR.
func (f *fakeTablet) send(es ...Event) {
	qp := f.at(f.desc)
	for _, e := range es {
		avail := qp[64*16:]
		head := le.Uint16(avail[4+2*(f.availSeen%64):])
		f.availSeen++
		buf := f.at(le.Uint64(qp[16*int(head):]))
		le.PutUint16(buf, e.Type)
		le.PutUint16(buf[2:], e.Code)
		le.PutUint32(buf[4:], uint32(e.Value))
		u := f.at(f.used)
		le.PutUint32(u[4+8*int(f.usedIdx%64):], uint32(head))
		le.PutUint32(u[8+8*int(f.usedIdx%64):], eventBytes)
		f.usedIdx++
		le.PutUint16(u[2:], f.usedIdx)
	}
	f.isr.b[0] = virtio.ISRQueue
}

// W22-S04: the tablet's name and axes, its queue filled, events handed on in
// order across the ring's wrap, each buffer offered again; an interrupt with
// a clear ISR (another device's, on a shared line) is not claimed.
func TestTabletEvents(t *testing.T) {
	f := newFakeTablet()
	f.isr.onRead = func(uint32) {}
	dev := &virtio.Device{Common: &f.common, Notify: &f.notify, ISR: &f.isr, Config: &f.cfg, NotifyMultiplier: 4}
	var got []Event
	tab, err := New(dev, f.dma, func(e Event) { got = append(got, e) })
	if err != nil {
		t.Fatal(err)
	}
	if tab.Name != "QEMU Virtio Tablet" || !tab.HasAbs || tab.X.String() != "0-32767" || tab.Y.Max != 32767 {
		t.Fatalf("tablet %q abs %v x %v y %v", tab.Name, tab.HasAbs, tab.X, tab.Y)
	}
	if st := f.common.b[0x14]; st&virtio.StatusDriverOK == 0 {
		t.Errorf("status %#x", st)
	}
	if avail := le.Uint16(f.at(f.desc)[64*16+2:]); avail != 64 {
		t.Errorf("%d buffers offered", avail)
	}
	f.isr.onRead = nil
	if tab.Interrupt() {
		t.Error("claimed an interrupt with the ISR clear")
	}
	f.isr.onRead = func(uint32) {}
	for round := 0; round < 3; round++ {
		var want []Event
		for i := 0; i < 50; i++ {
			want = append(want, Event{3, uint16(i & 1), int32(round*1000 + i)})
		}
		got = got[:0]
		f.send(want...)
		f.isr.onRead = func(uint32) {} // the read clears it on the device
		if !tab.Interrupt() {
			t.Fatal("did not claim its interrupt")
		}
		f.isr.b[0] = 0
		if len(got) != 50 || got[0] != want[0] || got[49] != want[49] {
			t.Fatalf("round %d: %d events, %v", round, len(got), got[:min(3, len(got))])
		}
	}
	if avail := le.Uint16(f.at(f.desc)[64*16+2:]); avail != 64+150 {
		t.Errorf("buffers not offered again: avail %d", avail)
	}
}
