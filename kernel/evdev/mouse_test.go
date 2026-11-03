package evdev

import (
	"reflect"
	"testing"
)

// eviocgbit is _IOR('E', 0x20 + ev, size).
func eviocgbit(ev, size uint64) uint64 { return 0x80000000 | size<<16 | 'E'<<8 | 0x20 + ev }

func bitSet(b []byte, n int) bool { return n/8 < len(b) && b[n/8]>>(n%8)&1 != 0 }

func bitsOf(t *testing.T, h *handle, ev uint64) []byte {
	t.Helper()
	var got []byte
	out := func(_ uint64, b []byte) error { got = append([]byte(nil), b...); return nil }
	if _, err := h.Ioctl(eviocgbit(ev, 96), 0, nil, out); err != nil {
		t.Fatalf("EVIOCGBIT(%d): %v", ev, err)
	}
	return got
}

// Toolkits classify devices by EVIOCGBIT: the keyboard has keys and no
// relative axes, the mouse has REL_X/REL_Y and buttons but no keys.
func TestClassification(t *testing.T) {
	kbd, err := New(clock).Open()
	if err != nil {
		t.Fatal(err)
	}
	m, err := NewMouse(clock, nil).Open()
	if err != nil {
		t.Fatal(err)
	}
	k, mo := kbd.(*handle), m.(*handle)
	if ty := bitsOf(t, k, 0); !bitSet(ty, EvKey) || bitSet(ty, EvRel) {
		t.Errorf("keyboard types %08b", ty[0])
	}
	if keys := bitsOf(t, k, EvKey); !bitSet(keys, 30) || !bitSet(keys, 103) || bitSet(keys, BtnLeft) {
		t.Error("keyboard keys: want KEY_A and KEY_UP, no BTN_LEFT")
	}
	if ty := bitsOf(t, mo, 0); !bitSet(ty, EvKey) || !bitSet(ty, EvRel) {
		t.Errorf("mouse types %08b", ty[0])
	}
	if rel := bitsOf(t, mo, EvRel); !bitSet(rel, RelX) || !bitSet(rel, RelY) || !bitSet(rel, RelWheel) {
		t.Errorf("mouse axes %08b %08b", rel[0], rel[1])
	}
	if keys := bitsOf(t, mo, EvKey); !bitSet(keys, BtnLeft) || !bitSet(keys, BtnMiddle) || bitSet(keys, 30) {
		t.Error("mouse keys: want BTN_LEFT and BTN_MIDDLE, no KEY_A")
	}
}

func TestMouseEvents(t *testing.T) {
	mo := NewMouse(clock, nil)
	h, _ := mo.Open()
	mo.Feed(5, -3, 0, ButtonLeft)
	mo.Feed(0, 0, 1, ButtonLeft)
	mo.Feed(0, 0, 0, 0)
	mo.Feed(0, 0, 0, 0) // nothing changed: no events
	b := make([]byte, 16*EventSize)
	n, err := h.Read(b)
	if err != nil {
		t.Fatal(err)
	}
	want := []ev{
		{EvRel, RelX, 5}, {EvRel, RelY, -3}, {EvKey, BtnLeft, 1}, {EvSyn, SynReport, 0},
		{EvRel, RelWheel, 1}, {EvSyn, SynReport, 0},
		{EvKey, BtnLeft, 0}, {EvSyn, SynReport, 0},
	}
	if got := decode(t, b[:n]); !reflect.DeepEqual(got, want) {
		t.Errorf("events\n got %v\nwant %v", got, want)
	}
}

func miceRead(t *testing.T, h *miceHandle) []byte {
	t.Helper()
	b := make([]byte, 64)
	n, err := h.Read(b)
	if err != nil {
		t.Fatal(err)
	}
	return b[:n]
}

// /dev/input/mice speaks PS/2: 3-byte packets with y up, big moves split,
// and 4-byte packets with the wheel after the IntelliMouse handshake.
func TestMice(t *testing.T) {
	mice := NewMice()
	mo := NewMouse(clock, mice)
	d, _ := mice.Open()
	h := d.(*miceHandle)
	mo.Feed(-2, 4, 0, ButtonRight)
	if got, want := miceRead(t, h), []byte{0x08 | 0x02 | 0x10 | 0x20, 0xFE, 0xFC}; !reflect.DeepEqual(got, want) {
		t.Errorf("packet % x, want % x", got, want)
	}
	mo.Feed(200, 0, 0, 0)
	if got, want := miceRead(t, h), []byte{0x08, 127, 0, 0x08, 73, 0}; !reflect.DeepEqual(got, want) {
		t.Errorf("split move % x, want % x", got, want)
	}
	mo.Feed(1, 0, 0, 0)
	h.Write([]byte{0xF3, 200, 0xF3, 100, 0xF3, 80, 0xF2}) // only the last answer stays, read first (BUG-0018)
	if got, want := miceRead(t, h), []byte{0xFA, 0x03}; !reflect.DeepEqual(got, want) {
		t.Errorf("handshake answer % x, want % x", got, want)
	}
	if got, want := miceRead(t, h), []byte{0x08, 1, 0}; !reflect.DeepEqual(got, want) {
		t.Errorf("motion after the answer % x, want % x", got, want)
	}
	mo.Feed(0, 0, 1, 0) // wheel up: -1 in the fourth byte, as a PS/2 mouse sends
	if got, want := miceRead(t, h), []byte{0x08, 0, 0, 0xFF}; !reflect.DeepEqual(got, want) {
		t.Errorf("wheel packet % x, want % x", got, want)
	}
}

// BUG-0018: with bytes waiting, Poll said POLLIN and not POLLOUT, so a
// nonblocking writer (TinyX sending the IntelliMouse handshake one byte
// at a time) got EAGAIN after the first ACK and never finished.
func TestMiceAlwaysWritable(t *testing.T) {
	d, _ := NewMice().Open()
	h := d.(*miceHandle)
	h.Write([]byte{0xF3})
	if rev, _ := h.Poll(); rev != 0x1|0x4 {
		t.Errorf("Poll with an ACK queued = %#x, want POLLIN|POLLOUT", rev)
	}
	miceRead(t, h)
	if rev, _ := h.Poll(); rev != 0x4 {
		t.Errorf("Poll with nothing queued = %#x, want POLLOUT", rev)
	}
}
