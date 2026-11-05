// dsp.go - /dev/dsp (W22-S02): the subset of OSS that small programs and
// SDL's dsp driver use. One opener at a time (EBUSY), as OSS without a
// mixing daemon; write blocks while the card's ring is full (Poller gives
// O_NONBLOCK and poll its POLLOUT); close drains what was written. Never
// touches the card itself: everything goes through Output.
package sound

import (
	"encoding/binary"
	"io"
	"sync"
	"syscall"

	"burrow/kernel/fs/devfs"
)

// OSS ioctl requests (<linux/soundcard.h>), checked on Burrow by user/tone.c,
// which is built against that header.
const (
	SNDCTL_DSP_RESET       = 0x00005000
	SNDCTL_DSP_SYNC        = 0x00005001
	SNDCTL_DSP_SPEED       = 0xC0045002
	SNDCTL_DSP_STEREO      = 0xC0045003
	SNDCTL_DSP_GETBLKSIZE  = 0xC0045004
	SNDCTL_DSP_SETFMT      = 0xC0045005
	SNDCTL_DSP_CHANNELS    = 0xC0045006
	SNDCTL_DSP_POST        = 0x00005008
	SNDCTL_DSP_SETFRAGMENT = 0xC004500A
	SNDCTL_DSP_GETFMTS     = 0x8004500B
	SNDCTL_DSP_GETOSPACE   = 0x8010500C
	SNDCTL_DSP_GETCAPS     = 0x8004500F
	SNDCTL_DSP_GETODELAY   = 0x80045017

	pollOut = 0x4

	defaultRate = 8000 // OSS's: 8 kHz, unsigned 8-bit, mono
)

// Output is a card's playback, in its frames (16-bit stereo).
type Output interface {
	SetRate(hz int) int               // the rate the card took
	Write(frames []byte) (int, error) // blocks while the ring is full
	Space() (free, total, size int)   // free and total buffers, bytes in one
	Delay() int                       // bytes written and not yet played
	Drain()                           // returns when everything written has played
	Reset()                           // drops what is queued
	Changed() <-chan struct{}         // closed when space frees up
}

// DSP is the /dev/dsp device.
type DSP struct {
	out  Output
	mu   sync.Mutex
	open bool
}

func NewDSP(out Output) *DSP { return &DSP{out: out} }

// Open gives one opener its handle (devfs.Opener); a second gets EBUSY.
func (d *DSP) Open() (devfs.Device, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.open {
		return nil, syscall.EBUSY
	}
	d.open = true
	h := &handle{d: d, f: Format{AFMT_U8, 1}}
	h.rate = d.out.SetRate(defaultRate)
	return h, nil
}

func (d *DSP) Read([]byte) (int, error)  { return 0, io.EOF }
func (d *DSP) Write([]byte) (int, error) { return 0, syscall.EINVAL }

type handle struct {
	d       *DSP
	mu      sync.Mutex
	f       Format
	rate    int
	partial []byte // the start of a frame the last write ended in
	buf     []byte
	once    sync.Once
}

func (h *handle) Read([]byte) (int, error) { return 0, syscall.EINVAL } // no recording

// Write converts p to the card's frames and queues them, blocking while
// the ring is full; it always takes all of p.
func (h *handle) Write(p []byte) (int, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	src := p
	if len(h.partial) > 0 {
		src = append(h.partial, p...)
	}
	frames, used := h.f.Convert(h.buf[:0], src)
	h.buf = frames
	h.partial = append(h.partial[:0], src[used:]...)
	if len(frames) > 0 {
		if _, err := h.d.out.Write(frames); err != nil {
			return 0, err
		}
	}
	return len(p), nil
}

// Close drains what was written, then lets the next opener in.
func (h *handle) Close() error {
	h.once.Do(func() {
		h.d.out.Drain()
		h.d.mu.Lock()
		h.d.open = false
		h.d.mu.Unlock()
	})
	return nil
}

// Poll: writable while a buffer is free (proc's Poller).
func (h *handle) Poll() (int16, <-chan struct{}) {
	ch := h.d.out.Changed()
	if free, _, _ := h.d.out.Space(); free > 0 {
		return pollOut, ch
	}
	return 0, ch
}

// progBytes turns card bytes into the program's (its frame size against 4).
func (h *handle) progBytes(cardBytes int) int {
	return cardBytes / CardFrameBytes * h.f.FrameBytes()
}

// Ioctl answers OSS's playback requests.
func (h *handle) Ioctl(req, arg uint64, in, out func(va uint64, b []byte) error) (int64, error) {
	le := binary.LittleEndian
	getInt := func() (int, error) {
		b := make([]byte, 4)
		err := in(arg, b)
		return int(int32(le.Uint32(b))), err
	}
	putInts := func(vs ...int) error {
		b := make([]byte, 4*len(vs))
		for i, v := range vs {
			le.PutUint32(b[4*i:], uint32(int32(v)))
		}
		return out(arg, b)
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	switch req {
	case SNDCTL_DSP_RESET:
		h.d.out.Reset()
		h.partial = h.partial[:0]
		return 0, nil
	case SNDCTL_DSP_SYNC, SNDCTL_DSP_POST:
		if req == SNDCTL_DSP_SYNC {
			h.d.out.Drain()
		}
		return 0, nil
	case SNDCTL_DSP_SPEED:
		v, err := getInt()
		if err != nil {
			return 0, err
		}
		h.rate = h.d.out.SetRate(v)
		return 0, putInts(h.rate)
	case SNDCTL_DSP_SETFMT:
		v, err := getInt()
		if err != nil {
			return 0, err
		}
		if v == AFMT_U8 || v == AFMT_S16_LE {
			h.f.Fmt, h.partial = v, h.partial[:0]
		}
		return 0, putInts(h.f.Fmt) // the format in force: the nearest OSS can do
	case SNDCTL_DSP_CHANNELS, SNDCTL_DSP_STEREO:
		v, err := getInt()
		if err != nil {
			return 0, err
		}
		if req == SNDCTL_DSP_STEREO {
			v++ // 0 mono, 1 stereo
		}
		if v == 1 || v == 2 {
			h.f.Channels, h.partial = v, h.partial[:0]
		}
		if req == SNDCTL_DSP_STEREO {
			return 0, putInts(h.f.Channels - 1)
		}
		return 0, putInts(h.f.Channels)
	case SNDCTL_DSP_GETFMTS:
		return 0, putInts(AFMT_U8 | AFMT_S16_LE)
	case SNDCTL_DSP_GETCAPS:
		return 0, putInts(0) // no duplex, no trigger, no mmap
	case SNDCTL_DSP_SETFRAGMENT:
		return 0, nil // the card's buffers are fixed: accepted, ignored
	case SNDCTL_DSP_GETBLKSIZE:
		_, _, size := h.d.out.Space()
		return 0, putInts(h.progBytes(size))
	case SNDCTL_DSP_GETOSPACE: // audio_buf_info: fragments, fragstotal, fragsize, bytes
		free, total, size := h.d.out.Space()
		return 0, putInts(free, total, h.progBytes(size), h.progBytes(free*size))
	case SNDCTL_DSP_GETODELAY:
		return 0, putInts(h.progBytes(h.d.out.Delay()))
	}
	return 0, syscall.ENOTTY
}
