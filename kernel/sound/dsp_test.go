package sound

import (
	"bytes"
	"encoding/binary"
	"sync"
	"syscall"
	"testing"
)

// fakeOut is a card that keeps every frame written.
type fakeOut struct {
	mu      sync.Mutex
	got     bytes.Buffer
	rate    int
	drains  int
	resets  int
	changed chan struct{}
}

func (o *fakeOut) SetRate(hz int) int {
	if hz > 48000 {
		hz = 48000
	}
	o.rate = hz
	return hz
}
func (o *fakeOut) Write(p []byte) (int, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.got.Write(p)
}
func (o *fakeOut) Space() (int, int, int)   { return 30, 32, 4096 }
func (o *fakeOut) Delay() int               { return 8192 }
func (o *fakeOut) Drain()                   { o.drains++ }
func (o *fakeOut) Reset()                   { o.resets++ }
func (o *fakeOut) Changed() <-chan struct{} { return o.changed }

type mem struct{ b []byte }

func (m *mem) in(va uint64, b []byte) error  { copy(b, m.b); return nil }
func (m *mem) out(va uint64, b []byte) error { m.b = append([]byte(nil), b...); return nil }

func ioctlInt(t *testing.T, h *handle, req uint64, v int) int {
	t.Helper()
	m := &mem{b: binary.LittleEndian.AppendUint32(nil, uint32(int32(v)))}
	if _, err := h.Ioctl(req, 0x1000, m.in, m.out); err != nil {
		t.Fatalf("ioctl %#x: %v", req, err)
	}
	return int(int32(binary.LittleEndian.Uint32(m.b)))
}

func TestConvertToCardFrames(t *testing.T) {
	out, used := Format{AFMT_U8, 1}.Convert(nil, []byte{0x80, 0xFF, 0x00})
	if used != 3 || !bytes.Equal(out, []byte{0, 0, 0, 0, 0x00, 0x7F, 0x00, 0x7F, 0x00, 0x80, 0x00, 0x80}) {
		t.Errorf("u8 mono: %d % x", used, out)
	}
	out, used = Format{AFMT_S16_LE, 1}.Convert(nil, []byte{0x34, 0x12, 0xFF})
	if used != 2 || !bytes.Equal(out, []byte{0x34, 0x12, 0x34, 0x12}) {
		t.Errorf("s16 mono with a partial frame: %d % x", used, out)
	}
	out, used = Format{AFMT_S16_LE, 2}.Convert(nil, []byte{1, 2, 3, 4, 5})
	if used != 4 || !bytes.Equal(out, []byte{1, 2, 3, 4}) {
		t.Errorf("s16 stereo: %d % x", used, out)
	}
}

// W22-S02: OSS's defaults, its set-up ioctls answering what is in force,
// partial frames carried across writes, and one opener at a time.
func TestDSPHandle(t *testing.T) {
	o := &fakeOut{changed: make(chan struct{})}
	d := NewDSP(o)
	dev, err := d.Open()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := d.Open(); err != syscall.EBUSY {
		t.Errorf("second open: %v, want EBUSY", err)
	}
	h := dev.(*handle)
	if o.rate != 8000 || h.f != (Format{AFMT_U8, 1}) {
		t.Errorf("defaults: %d Hz %+v", o.rate, h.f)
	}
	if got := ioctlInt(t, h, SNDCTL_DSP_SETFMT, AFMT_S16_LE); got != AFMT_S16_LE {
		t.Errorf("SETFMT = %#x", got)
	}
	if got := ioctlInt(t, h, SNDCTL_DSP_SETFMT, 0x20); got != AFMT_S16_LE { // AFMT_S16_BE: not done
		t.Errorf("SETFMT of an unknown format = %#x, want the one in force", got)
	}
	if got := ioctlInt(t, h, SNDCTL_DSP_CHANNELS, 2); got != 2 {
		t.Errorf("CHANNELS = %d", got)
	}
	if got := ioctlInt(t, h, SNDCTL_DSP_SPEED, 96000); got != 48000 {
		t.Errorf("SPEED 96000 = %d, want the card's 48000", got)
	}
	h.Write([]byte{1, 2, 3})       // three bytes of a four-byte frame
	h.Write([]byte{4, 5, 6, 7, 8}) // completes it and one more
	if !bytes.Equal(o.got.Bytes(), []byte{1, 2, 3, 4, 5, 6, 7, 8}) {
		t.Errorf("card got % x", o.got.Bytes())
	}
	m := &mem{}
	if _, err := h.Ioctl(SNDCTL_DSP_GETOSPACE, 0x1000, m.in, m.out); err != nil || len(m.b) != 16 {
		t.Fatalf("GETOSPACE: %v %d bytes", err, len(m.b))
	}
	le := binary.LittleEndian
	if le.Uint32(m.b) != 30 || le.Uint32(m.b[4:]) != 32 || le.Uint32(m.b[8:]) != 4096 || le.Uint32(m.b[12:]) != 30*4096 {
		t.Errorf("audio_buf_info % x", m.b)
	}
	ioctlInt(t, h, SNDCTL_DSP_CHANNELS, 1) // mono 16-bit: half the card's bytes
	if got := ioctlInt(t, h, SNDCTL_DSP_GETODELAY, 0); got != 4096 {
		t.Errorf("GETODELAY = %d, want 8192 card bytes as 4096 of mono", got)
	}
	if _, err := h.Ioctl(0x5099, 0, m.in, m.out); err != syscall.ENOTTY {
		t.Errorf("unknown ioctl: %v", err)
	}
	if rev, _ := h.Poll(); rev != pollOut {
		t.Errorf("Poll = %#x, want POLLOUT", rev)
	}
	h.Close()
	if o.drains != 1 {
		t.Errorf("close drained %d times", o.drains)
	}
	if _, err := d.Open(); err != nil {
		t.Errorf("open after close: %v", err)
	}
}
