// Package fbdev is /dev/fb0 (W11-S01, E10.F01): the subset of Linux's
// framebuffer device that Doom-style programs use, over a linear
// framebuffer at a known physical address, in the depth and colour layout
// the firmware set (W21-S03; XRGB8888 when the Mode does not say). It answers FBIOGET_VSCREENINFO,
// FBIOGET_FSCREENINFO, FBIOPUT_VSCREENINFO (the current mode only),
// FBIOPAN_DISPLAY (offset 0), and since W14-S08 FBIOGETCMAP/FBIOPUTCMAP (a
// truecolour mode has no palette: reads give a linear ramp, writes are
// accepted) and FBIOBLANK; it offers its memory to mmap(MAP_SHARED).
// It never changes the video mode and never touches pixels itself; while
// it is open, whoever else draws on the screen (the text console) is told
// to stand aside through hold. Layouts: <linux/fb.h>. Docs:
// docs/04-plan/waves/WAVE-11-doom.md.
package fbdev

import (
	"encoding/binary"
	"io"
	"sync"
	"syscall"

	"burrow/kernel/fs/devfs"
)

// Linux ioctl numbers and constants (<linux/fb.h>).
const (
	FBIOGET_VSCREENINFO = 0x4600
	FBIOPUT_VSCREENINFO = 0x4601
	FBIOGET_FSCREENINFO = 0x4602
	FBIOPAN_DISPLAY     = 0x4606
	FBIOGETCMAP         = 0x4604
	FBIOPUTCMAP         = 0x4605
	FBIOBLANK           = 0x4611

	cmapSize       = 40 // struct fb_cmap {u32 start, len; u16 *red, *green, *blue, *transp}
	cmapMaxEntries = 256
	blankPowerdown = 4 // FB_BLANK_POWERDOWN, the deepest

	VarSize = 160 // sizeof(struct fb_var_screeninfo)
	FixSize = 80  // sizeof(struct fb_fix_screeninfo) on x86-64

	typePackedPixels = 0
	visualTrueColor  = 2
	bitsPerPixel     = 32
)

// Mode is the framebuffer as the hardware set it up.
type Mode struct {
	Phys          uint64 // physical address of pixel (0, 0)
	Width, Height int
	PitchBytes    int // bytes from one row to the next (PIT-27)
	// The pixel layout (W21-S03); BitsPerPixel 0 means XRGB8888.
	BitsPerPixel     int
	Red, Green, Blue Channel
}

// Channel is a colour field, as struct fb_bitfield has it: Length bits
// from bit Offset up.
type Channel struct{ Offset, Length uint8 }

// layout is m's depth and colour fields, XRGB8888 unless m says otherwise.
func (m Mode) layout() (bpp uint32, r, g, b Channel) {
	if m.BitsPerPixel == 0 {
		return bitsPerPixel, Channel{16, 8}, Channel{8, 8}, Channel{0, 8}
	}
	return uint32(m.BitsPerPixel), m.Red, m.Green, m.Blue
}

// Size is how many bytes of memory the mode covers (smem_len).
func (m Mode) Size() uint64 { return uint64(m.PitchBytes) * uint64(m.Height) }

// Device is /dev/fb0. hold(true) runs when the first open arrives and
// hold(false) when the last one closes.
type Device struct {
	mode  Mode
	hold  func(held bool)
	mu    sync.Mutex
	opens int
}

func New(mode Mode, hold func(held bool)) *Device {
	if hold == nil {
		hold = func(bool) {}
	}
	return &Device{mode: mode, hold: hold}
}

// Open gives each open file its own handle (devfs.Opener).
func (d *Device) Open() (devfs.Device, error) {
	d.mu.Lock()
	d.opens++
	first := d.opens == 1
	d.mu.Unlock()
	if first {
		d.hold(true)
	}
	return &handle{d: d}, nil
}

// Held reports whether some program has the framebuffer open.
func (d *Device) Held() bool { d.mu.Lock(); defer d.mu.Unlock(); return d.opens > 0 }

// The device file itself (never opened through devfs.Opener) reads as empty.
func (d *Device) Read([]byte) (int, error)  { return 0, io.EOF }
func (d *Device) Write([]byte) (int, error) { return 0, syscall.EINVAL }

// handle is one open /dev/fb0. Pixels are reached by mmap, not read/write.
type handle struct {
	d    *Device
	once sync.Once
}

func (h *handle) Read([]byte) (int, error)  { return 0, io.EOF }
func (h *handle) Write([]byte) (int, error) { return 0, syscall.EINVAL }

func (h *handle) Close() error {
	h.once.Do(func() {
		d := h.d
		d.mu.Lock()
		d.opens--
		last := d.opens == 0
		d.mu.Unlock()
		if last {
			d.hold(false)
		}
	})
	return nil
}

// PhysRange is the memory mmap maps (proc's Mappable).
func (h *handle) PhysRange() (pa, size uint64) { return h.d.mode.Phys, h.d.mode.Size() }

// WriteCombined: the kernel maps the framebuffer write-combining, so a
// program's mapping must be too (proc's WriteCombined, W21-S04).
func (h *handle) WriteCombined() bool { return true }

// Ioctl answers the fbdev requests; in and out copy the caller's memory.
func (h *handle) Ioctl(req, arg uint64, in, out func(va uint64, b []byte) error) (int64, error) {
	m := h.d.mode
	switch req {
	case FBIOGET_VSCREENINFO:
		return 0, out(arg, Var(m))
	case FBIOGET_FSCREENINFO:
		return 0, out(arg, Fix(m))
	case FBIOPUT_VSCREENINFO, FBIOPAN_DISPLAY:
		b := make([]byte, VarSize)
		if err := in(arg, b); err != nil {
			return 0, err
		}
		le := binary.LittleEndian
		bpp, _, _, _ := m.layout()
		same := le.Uint32(b[0:]) == uint32(m.Width) && le.Uint32(b[4:]) == uint32(m.Height) &&
			le.Uint32(b[24:]) == bpp && le.Uint32(b[16:]) == 0 && le.Uint32(b[20:]) == 0
		if req == FBIOPAN_DISPLAY {
			same = le.Uint32(b[16:]) == 0 && le.Uint32(b[20:]) == 0
		}
		if !same {
			return 0, syscall.EINVAL // one mode, no panning
		}
		if req == FBIOPUT_VSCREENINFO {
			return 0, out(arg, Var(m)) // the driver writes back what it set
		}
		return 0, nil
	case FBIOGETCMAP, FBIOPUTCMAP:
		return 0, cmap(req, arg, in, out)
	case FBIOBLANK:
		if arg > blankPowerdown {
			return 0, syscall.EINVAL
		}
		return 0, nil // the BGA has no blanking; the picture stays
	}
	return 0, syscall.ENOTTY
}

// cmap answers FBIOGETCMAP with Linux's default ramp for a truecolour mode
// (entry i is i * 0x101 in every colour, no transparency) and accepts
// FBIOPUTCMAP, which changes nothing.
func cmap(req, arg uint64, in, out func(va uint64, b []byte) error) error {
	b := make([]byte, cmapSize)
	if err := in(arg, b); err != nil {
		return err
	}
	le := binary.LittleEndian
	start, n := le.Uint32(b[0:]), le.Uint32(b[4:])
	if start+n > cmapMaxEntries || start+n < start {
		return syscall.EINVAL
	}
	if req == FBIOPUTCMAP {
		return nil
	}
	ramp := make([]byte, 2*n)
	for i := uint32(0); i < n; i++ {
		le.PutUint16(ramp[2*i:], uint16((start+i)*0x101))
	}
	for _, off := range []int{8, 16, 24} { // red, green, blue
		if va := le.Uint64(b[off:]); va != 0 {
			if err := out(va, ramp); err != nil {
				return err
			}
		}
	}
	if va := le.Uint64(b[32:]); va != 0 {
		return out(va, make([]byte, 2*n))
	}
	return nil
}

// Var is struct fb_var_screeninfo for m: its depth and colour fields, no
// virtual screen, no transparency.
func Var(m Mode) []byte {
	b := make([]byte, VarSize)
	le := binary.LittleEndian
	bpp, r, g, bl := m.layout()
	for i, v := range []uint32{
		uint32(m.Width), uint32(m.Height), uint32(m.Width), uint32(m.Height), // xres yres xres_virtual yres_virtual
		0, 0, bpp, 0, // xoffset yoffset bits_per_pixel grayscale
		uint32(r.Offset), uint32(r.Length), 0, // red: offset length msb_right
		uint32(g.Offset), uint32(g.Length), 0, // green
		uint32(bl.Offset), uint32(bl.Length), 0, // blue
		transpOffset(bpp), 0, 0, // transp: none
	} {
		le.PutUint32(b[4*i:], v)
	}
	le.PutUint32(b[88:], ^uint32(0)) // height in mm: unknown
	le.PutUint32(b[92:], ^uint32(0)) // width in mm: unknown
	return b
}

// transpOffset is where a 32-bit pixel's spare byte is (length 0: unused),
// as before W21; other depths have none.
func transpOffset(bpp uint32) uint32 {
	if bpp == 32 {
		return 24
	}
	return 0
}

// Fix is struct fb_fix_screeninfo for m.
func Fix(m Mode) []byte {
	b := make([]byte, FixSize)
	le := binary.LittleEndian
	copy(b[0:16], "burrow-bga")
	le.PutUint64(b[16:], m.Phys)           // smem_start
	le.PutUint32(b[24:], uint32(m.Size())) // smem_len
	le.PutUint32(b[28:], typePackedPixels)
	le.PutUint32(b[36:], visualTrueColor)
	le.PutUint32(b[48:], uint32(m.PitchBytes)) // line_length
	return b
}
