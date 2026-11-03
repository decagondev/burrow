package fbdev

import (
	"encoding/binary"
	"errors"
	"syscall"
	"testing"

	"burrow/kernel/fs/devfs"
)

var mode = Mode{Phys: 0xFD000000, Width: 1024, Height: 768, PitchBytes: 4096}

// mem is a caller's memory for Ioctl: one buffer at address 0x1000.
type mem struct{ b []byte }

func (m *mem) in(va uint64, b []byte) error {
	if va != 0x1000 || len(b) > len(m.b) {
		return syscall.EFAULT
	}
	copy(b, m.b)
	return nil
}

func (m *mem) out(va uint64, b []byte) error {
	if va != 0x1000 {
		return syscall.EFAULT
	}
	m.b = append([]byte(nil), b...)
	return nil
}

func open(t *testing.T, d *Device) *handle {
	t.Helper()
	h, err := d.Open()
	if err != nil {
		t.Fatal(err)
	}
	return h.(*handle)
}

// Offsets are <linux/fb.h>'s on x86-64 (user/fbtest.c prints them on Burrow).
func TestScreenInfoLayout(t *testing.T) {
	h, m := open(t, New(mode, nil)), &mem{}
	le := binary.LittleEndian
	if _, err := h.Ioctl(FBIOGET_VSCREENINFO, 0x1000, m.in, m.out); err != nil || len(m.b) != VarSize {
		t.Fatalf("VSCREENINFO: %v, %d bytes", err, len(m.b))
	}
	for _, c := range []struct {
		name     string
		off, val uint32
	}{{"xres", 0, 1024}, {"yres", 4, 768}, {"xres_virtual", 8, 1024}, {"bits_per_pixel", 24, 32},
		{"red.offset", 32, 16}, {"red.length", 36, 8}, {"green.offset", 44, 8}, {"blue.offset", 56, 0},
		{"blue.length", 60, 8}, {"transp.length", 72, 0}} {
		if got := le.Uint32(m.b[c.off:]); got != c.val {
			t.Errorf("%s = %d, want %d", c.name, got, c.val)
		}
	}
	if _, err := h.Ioctl(FBIOGET_FSCREENINFO, 0x1000, m.in, m.out); err != nil || len(m.b) != FixSize {
		t.Fatalf("FSCREENINFO: %v, %d bytes", err, len(m.b))
	}
	if le.Uint64(m.b[16:]) != 0xFD000000 || le.Uint32(m.b[24:]) != 4096*768 ||
		le.Uint32(m.b[36:]) != visualTrueColor || le.Uint32(m.b[48:]) != 4096 {
		t.Errorf("fix: smem_start %#x len %d visual %d line_length %d",
			le.Uint64(m.b[16:]), le.Uint32(m.b[24:]), le.Uint32(m.b[36:]), le.Uint32(m.b[48:]))
	}
	if pa, size := h.PhysRange(); pa != 0xFD000000 || size != 4096*768 {
		t.Errorf("PhysRange %#x %d", pa, size)
	}
}

func TestPutAndPan(t *testing.T) {
	h := open(t, New(mode, nil))
	m := &mem{b: Var(mode)}
	if _, err := h.Ioctl(FBIOPUT_VSCREENINFO, 0x1000, m.in, m.out); err != nil {
		t.Errorf("PUT of the current mode: %v", err)
	}
	if _, err := h.Ioctl(FBIOPAN_DISPLAY, 0x1000, m.in, m.out); err != nil {
		t.Errorf("PAN to 0: %v", err)
	}
	other := Var(Mode{Width: 640, Height: 480, PitchBytes: 2560})
	m.b = other
	if _, err := h.Ioctl(FBIOPUT_VSCREENINFO, 0x1000, m.in, m.out); !errors.Is(err, syscall.EINVAL) {
		t.Errorf("PUT of another mode: %v, want EINVAL", err)
	}
	if _, err := h.Ioctl(0x5401, 0x1000, m.in, m.out); !errors.Is(err, syscall.ENOTTY) {
		t.Errorf("TCGETS on fb0: %v, want ENOTTY", err)
	}
	if _, err := h.Ioctl(FBIOGET_VSCREENINFO, 0x2000, m.in, m.out); !errors.Is(err, syscall.EFAULT) {
		t.Errorf("bad address: %v, want EFAULT", err)
	}
}

func TestHoldFollowsOpens(t *testing.T) {
	var events []bool
	d := New(mode, func(held bool) { events = append(events, held) })
	fs := devfs.New()
	fs.Register("fb0", d, 0o660)
	a, _ := fs.Open("fb0")
	b, _ := fs.Open("fb0")
	if !d.Held() {
		t.Error("not held while open")
	}
	a.Close()
	a.Close() // closing twice counts once
	if len(events) != 1 || !d.Held() {
		t.Fatalf("after one of two closes: events %v held %v", events, d.Held())
	}
	b.Close()
	if len(events) != 2 || events[0] != true || events[1] != false || d.Held() {
		t.Errorf("events %v, held %v", events, d.Held())
	}
}
