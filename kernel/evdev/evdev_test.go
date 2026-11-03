package evdev

import (
	"encoding/binary"
	"errors"
	"syscall"
	"testing"
	"time"
)

func clock() time.Duration { return 3*time.Second + 250*time.Microsecond }

type ev struct {
	typ, code uint16
	value     int32
}

func decode(t *testing.T, b []byte) []ev {
	t.Helper()
	if len(b)%EventSize != 0 {
		t.Fatalf("%d bytes is not whole events", len(b))
	}
	le := binary.LittleEndian
	var out []ev
	for ; len(b) > 0; b = b[EventSize:] {
		if le.Uint64(b) != 3 || le.Uint64(b[8:]) != 250 {
			t.Errorf("timestamp %d.%06d", le.Uint64(b), le.Uint64(b[8:]))
		}
		out = append(out, ev{le.Uint16(b[16:]), le.Uint16(b[18:]), int32(le.Uint32(b[20:]))})
	}
	return out
}

func open(t *testing.T, d *Device) *handle {
	h, err := d.Open()
	if err != nil {
		t.Fatal(err)
	}
	return h.(*handle)
}

func TestKeyCodes(t *testing.T) {
	for _, c := range []struct {
		e0   bool
		sc   byte
		want uint16
	}{{false, 0x01, 1}, {false, 0x1C, 28}, {false, 0x1E, 30}, {false, 0x1D, 29}, {false, 0x39, 57},
		{false, 0x58, 88}, {false, 0x59, 0}, {false, 0x00, 0},
		{true, 0x48, 103}, {true, 0x50, 108}, {true, 0x4B, 105}, {true, 0x4D, 106}, {true, 0x1D, 97},
		{true, 0x1C, 96}, {true, 0x2A, 0}} {
		if got := KeyCode(c.e0, c.sc); got != c.want {
			t.Errorf("KeyCode(%v, %#x) = %d, want %d", c.e0, c.sc, got, c.want)
		}
	}
}

func TestPressRepeatRelease(t *testing.T) {
	d := New(clock)
	if d.Feed(0x1E) || d.Feed(0x9E) { // pressed and released before any open
		t.Error("grabbed with nothing open")
	}
	h := open(t, d)
	for _, sc := range []byte{0x1E, 0x1E, 0x9E, 0xE0, 0x48, 0xE0, 0xC8, 0xE0, 0x2A} {
		if !d.Feed(sc) {
			t.Fatalf("not grabbed while open (byte %#x)", sc)
		}
	}
	b := make([]byte, 20*EventSize)
	n, err := h.Read(b)
	if err != nil {
		t.Fatal(err)
	}
	want := []ev{{EvKey, 30, 1}, {EvSyn, 0, 0}, {EvKey, 30, 2}, {EvSyn, 0, 0}, {EvKey, 30, 0}, {EvSyn, 0, 0},
		{EvKey, 103, 1}, {EvSyn, 0, 0}, {EvKey, 103, 0}, {EvSyn, 0, 0}}
	got := decode(t, b[:n])
	if len(got) != len(want) {
		t.Fatalf("events %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("event %d = %v, want %v", i, got[i], want[i])
		}
	}
	h.Close()
	if d.Feed(0x1E) || d.Grabbed() {
		t.Error("still grabbed after close")
	}
}

func TestEachOpenHasItsQueue(t *testing.T) {
	d := New(clock)
	a, b := open(t, d), open(t, d)
	d.Feed(0x10)
	buf := make([]byte, EventSize) // room for one event: reads return whole events
	for _, h := range []*handle{a, b} {
		if n, _ := h.Read(buf); n != EventSize || decode(t, buf)[0] != (ev{EvKey, 16, 1}) {
			t.Errorf("first event %v", decode(t, buf))
		}
	}
	if _, err := a.Read(make([]byte, EventSize-1)); !errors.Is(err, syscall.EINVAL) {
		t.Errorf("short buffer: %v, want EINVAL", err)
	}
}

func TestBlockingReadAndPoll(t *testing.T) {
	d := New(clock)
	h := open(t, d)
	if rev, _ := h.Poll(); rev != 0 {
		t.Errorf("poll with nothing queued = %#x", rev)
	}
	cancel := make(chan struct{})
	close(cancel)
	if _, err := h.ReadCancel(make([]byte, EventSize), cancel); !errors.Is(err, syscall.EINTR) {
		t.Errorf("cancelled read: %v", err)
	}
	_, changed := h.Poll()
	got := make(chan int)
	go func() { n, _ := h.Read(make([]byte, 4*EventSize)); got <- n }()
	time.Sleep(10 * time.Millisecond)
	d.Feed(0x39)
	select {
	case <-changed:
	case <-time.After(time.Second):
		t.Fatal("poll channel not closed by an event")
	}
	if n := <-got; n != 2*EventSize {
		t.Errorf("blocked read returned %d bytes", n)
	}
}

func TestIoctls(t *testing.T) {
	h := open(t, New(clock))
	var got []byte
	out := func(_ uint64, b []byte) error { got = append([]byte(nil), b...); return nil }
	if n, err := h.Ioctl(0x80004506|32<<16, 0, nil, out); err != nil || string(got[:n-1]) != keyboardName {
		t.Errorf("EVIOCGNAME(32) = %d %q %v", n, got, err)
	}
	if n, _ := h.Ioctl(0x80004506|4<<16, 0, nil, out); n != 4 || string(got) != "Burr" {
		t.Errorf("EVIOCGNAME(4) = %d %q", n, got)
	}
	if _, err := h.Ioctl(eviocgversion, 0, nil, out); err != nil || binary.LittleEndian.Uint32(got) != version {
		t.Errorf("EVIOCGVERSION %x", got)
	}
	if _, err := h.Ioctl(0x4600, 0, nil, out); !errors.Is(err, syscall.ENOTTY) {
		t.Errorf("fbdev ioctl on evdev: %v", err)
	}
}
