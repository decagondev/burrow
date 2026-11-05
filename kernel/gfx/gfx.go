// Package gfx draws on a linear framebuffer (E07.F04): fills, a gradient,
// text in the built-in font, and a scrolling text console. It is pure Go
// over the device's bytes, so everything is tested on the host; the BGA
// driver hands it the real device memory and the mode the firmware set.
//
// Rows are Pitch bytes apart, which may be more than a row of pixels: always
// step by the pitch the device reports, never by the width (PIT-27). Pixels
// are packed by the mode's Format (W21-S03), never assumed to be XRGB.
package gfx

import (
	"fmt"
	"image"
	"unsafe"
)

// Color is 0x00RRGGBB, whatever the screen's own layout is (Format packs it).
type Color uint32

func RGB(r, g, b uint8) Color { return Color(r)<<16 | Color(g)<<8 | Color(b) }

func (c Color) RGB() (r, g, b uint8) { return uint8(c >> 16), uint8(c >> 8), uint8(c) }

// Framebuffer is one screen.
type Framebuffer struct {
	Width, Height int
	Pitch         int // bytes from one row to the next
	Format        Format
	Mem           []byte
	// The fast path for 4-byte pixels: Mem as words, Stride of them a row.
	Stride int
	Pix    []uint32 // nil for 2- and 3-byte pixels, or an odd pitch
}

// New checks that mem holds height rows of pitchBytes in format f.
func New(mem []byte, width, height, pitchBytes int, f Format) (*Framebuffer, error) {
	if err := f.check(); err != nil {
		return nil, err
	}
	n := f.Bytes
	if width <= 0 || height <= 0 || pitchBytes < width*n || n == 4 && pitchBytes%4 != 0 {
		return nil, fmt.Errorf("gfx: bad mode %dx%d pitch %d (%d bytes a pixel)", width, height, pitchBytes, n)
	}
	if len(mem) < pitchBytes*(height-1)+width*n {
		return nil, fmt.Errorf("gfx: %d bytes of memory for %dx%d pitch %d", len(mem), width, height, pitchBytes)
	}
	fb := &Framebuffer{Width: width, Height: height, Pitch: pitchBytes, Format: f, Mem: mem}
	if n == 4 && uintptr(unsafe.Pointer(&mem[0]))%4 == 0 {
		fb.Stride = pitchBytes / 4
		fb.Pix = unsafe.Slice((*uint32)(unsafe.Pointer(&mem[0])), len(mem)/4)
	}
	return fb, nil
}

func (f *Framebuffer) Bounds() image.Rectangle { return image.Rect(0, 0, f.Width, f.Height) }

func (f *Framebuffer) Set(x, y int, c Color) {
	if x >= 0 && y >= 0 && x < f.Width && y < f.Height {
		f.put(x, y, f.Format.Pack(c))
	}
}

func (f *Framebuffer) At(x, y int) Color { return f.Format.Unpack(f.raw(x, y)) }

// put stores a packed pixel (in bounds).
func (f *Framebuffer) put(x, y int, p uint32) {
	if f.Pix != nil {
		f.Pix[y*f.Stride+x] = p
		return
	}
	off := y*f.Pitch + x*f.Format.Bytes
	for i := 0; i < f.Format.Bytes; i++ {
		f.Mem[off+i] = byte(p >> (8 * i))
	}
}

// raw loads a packed pixel (in bounds).
func (f *Framebuffer) raw(x, y int) uint32 {
	if f.Pix != nil {
		return f.Pix[y*f.Stride+x]
	}
	off, p := y*f.Pitch+x*f.Format.Bytes, uint32(0)
	for i := 0; i < f.Format.Bytes; i++ {
		p |= uint32(f.Mem[off+i]) << (8 * i)
	}
	return p
}

// Fill paints r (clipped to the screen).
func (f *Framebuffer) Fill(r image.Rectangle, c Color) {
	r = r.Intersect(f.Bounds())
	p := f.Format.Pack(c)
	for y := r.Min.Y; y < r.Max.Y; y++ {
		if f.Pix != nil {
			row := f.Pix[y*f.Stride+r.Min.X : y*f.Stride+r.Max.X]
			for i := range row {
				row[i] = p
			}
			continue
		}
		for x := r.Min.X; x < r.Max.X; x++ {
			f.put(x, y, p)
		}
	}
}

// Gradient paints r from `from` at its left edge to `to` at its right edge.
func (f *Framebuffer) Gradient(r image.Rectangle, from, to Color) {
	r = r.Intersect(f.Bounds())
	w := r.Dx() - 1
	if w <= 0 {
		f.Fill(r, from)
		return
	}
	fr, fg, fb := from.RGB()
	tr, tg, tb := to.RGB()
	lerp := func(a, b uint8, x int) uint8 { return uint8(int(a) + (int(b)-int(a))*x/w) }
	for x := 0; x <= w; x++ {
		c := RGB(lerp(fr, tr, x), lerp(fg, tg, x), lerp(fb, tb, x))
		f.Fill(image.Rect(r.Min.X+x, r.Min.Y, r.Min.X+x+1, r.Max.Y), c)
	}
}

// DrawGlyph draws c with its top-left corner at (x, y), each font pixel a
// scale x scale square; bg < 0 leaves the cell's background as it is.
func (f *Framebuffer) DrawGlyph(x, y int, c byte, fg Color, bg int64, scale int) {
	g := glyph(c)
	for r := 0; r < CellH; r++ {
		var bits uint8
		if r < GlyphH {
			bits = g[r]
		}
		for col := 0; col < CellW; col++ {
			ink := col < GlyphW && bits&(1<<(GlyphW-1-col)) != 0
			if !ink && bg < 0 {
				continue
			}
			color := fg
			if !ink {
				color = Color(bg)
			}
			f.Fill(image.Rect(x+col*scale, y+r*scale, x+(col+1)*scale, y+(r+1)*scale), color)
		}
	}
}

// DrawText draws s on one line from (x, y) with a transparent background.
func (f *Framebuffer) DrawText(x, y int, s string, fg Color, scale int) {
	for i := 0; i < len(s); i++ {
		f.DrawGlyph(x+i*CellW*scale, y, s[i], fg, -1, scale)
	}
}
