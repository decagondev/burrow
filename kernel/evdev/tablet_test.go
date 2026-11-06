package evdev

import (
	"encoding/binary"
	"testing"
)

func newTestTablet(mice *Mice) *Tablet {
	return NewTablet(clock, "QEMU Virtio Tablet", [2]int32{0, 32767}, [2]int32{0, 32767}, mice,
		func() (int, int) { return 1024, 768 })
}

// absAt is the tablet value the harness sends for pixel p of size (its centre).
func absAt(p, size int) int32 { return int32((2*p + 1) * 32768 / (2 * size)) }

// W22-S05: the tablet is classified as an absolute pointer (EV_ABS ABS_X and
// ABS_Y, buttons, no relative axes), says its ranges and identity, and
// queues each packet whole at SYN_REPORT.
func TestTabletDevice(t *testing.T) {
	tab := newTestTablet(nil)
	o, err := tab.Open()
	if err != nil {
		t.Fatal(err)
	}
	h := o.(*handle)
	if ty := bitsOf(t, h, 0); !bitSet(ty, EvAbs) || !bitSet(ty, EvKey) || bitSet(ty, EvRel) {
		t.Errorf("types %08b", ty[0])
	}
	if a := bitsOf(t, h, EvAbs); !bitSet(a, AbsX) || !bitSet(a, AbsY) || bitSet(a, 2) {
		t.Errorf("abs bits %08b", a[0])
	}
	if k := bitsOf(t, h, EvKey); !bitSet(k, BtnLeft) {
		t.Error("no BTN_LEFT")
	}
	tab.Feed(EvAbs, AbsX, 1234)
	tab.Feed(EvAbs, AbsY, 4321)
	if n, _ := h.Poll(); n != 0 {
		t.Fatal("events queued before SYN_REPORT")
	}
	tab.Feed(EvSyn, SynReport, 0)
	buf := make([]byte, 10*EventSize)
	n, _ := h.Read(buf)
	if n != 3*EventSize {
		t.Fatalf("read %d bytes", n)
	}
	var info []byte
	out := func(_ uint64, b []byte) error { info = append([]byte(nil), b...); return nil }
	const eviocgabsY = 0x80000000 | 24<<16 | 'E'<<8 | 0x41
	if _, err := h.Ioctl(eviocgabsY, 0, nil, out); err != nil {
		t.Fatal(err)
	}
	le := binary.LittleEndian
	if v, mx := int32(le.Uint32(info)), int32(le.Uint32(info[8:])); v != 4321 || mx != 32767 {
		t.Errorf("EVIOCGABS(ABS_Y): value %d max %d", v, mx)
	}
	if _, err := h.Ioctl(0x80000000|24<<16|'E'<<8|0x42, 0, nil, out); err == nil {
		t.Error("EVIOCGABS of an axis it does not have")
	}
	var id []byte
	h.Ioctl(eviocgid, 0, nil, func(_ uint64, b []byte) error { id = b; return nil })
	if le.Uint16(id) != busVirtual || le.Uint16(id[2:]) != qemuVendor {
		t.Errorf("EVIOCGID % x", id)
	}
}

// moves reads mice packets (3 bytes) and sums them in screen directions.
func moves(t *testing.T, h *miceHandle) (dx, dy int, b0 byte) {
	t.Helper()
	h.mu.Lock()
	q := h.queue
	h.queue = nil
	h.mu.Unlock()
	for ; len(q) >= 3; q = q[3:] {
		dx += int(int8(q[1]))
		dy -= int(int8(q[2]))
		b0 = q[0]
	}
	return dx, dy, b0
}

// W22-S05: mice readers follow the tablet: a session starts at the screen's
// centre, each packet moves by the change of the scaled position, and an
// edge is overshot so a reader that lost packets clamps to it too.
func TestTabletMovesMice(t *testing.T) {
	mice := NewMice()
	tab := newTestTablet(mice)
	o, _ := mice.Open()
	h := o.(*miceHandle)
	put := func(x, y int, buttons int32) {
		tab.Feed(EvAbs, AbsX, absAt(x, 1024))
		tab.Feed(EvAbs, AbsY, absAt(y, 768))
		tab.Feed(EvKey, BtnLeft, buttons)
		tab.Feed(EvSyn, SynReport, 0)
	}
	put(612, 300, 0)
	if dx, dy, _ := moves(t, h); dx != 100 || dy != -84 {
		t.Errorf("first move %d,%d, want 100,-84 from the centre", dx, dy)
	}
	put(96, 32, 1)
	if dx, dy, b0 := moves(t, h); dx != -516 || dy != -268 || b0&1 == 0 {
		t.Errorf("move %d,%d buttons %#x", dx, dy, b0)
	}
	put(0, 767, 0)
	if dx, dy, _ := moves(t, h); dx != -96-1024 || dy != 735+768 {
		t.Errorf("to the corner %d,%d: want an overshoot by a screen", dx, dy)
	}
	h.Close()
	o, _ = mice.Open() // a new session: the reader starts at the centre again
	h = o.(*miceHandle)
	put(522, 394, 0)
	if dx, dy, _ := moves(t, h); dx != 10 || dy != 10 {
		t.Errorf("new session %d,%d", dx, dy)
	}
	for x := 0; x < 1024; x += 37 {
		if got := scale(absAt(x, 1024), 0, 32767, 1024); got != x {
			t.Fatalf("pixel %d scales back to %d", x, got)
		}
	}
}
