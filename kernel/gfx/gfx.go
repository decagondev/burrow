// Package gfx draws on a linear 32-bit framebuffer (E07.F04): fills, a
// gradient, text in the built-in font, and a scrolling text console. It is
// pure Go over a []uint32, so everything is tested on the host; the BGA
// driver hands it the real device memory.
//
// Rows are Stride pixels apart, which may be more than Width: always step
// by the pitch the device reports, never by the width (PIT-27).
package gfx

import (
	"fmt"
	"image"
	"unsafe"
)

// Color is 0x00RRGGBB, the BGA's 32 bpp layout (bytes B, G, R, unused).
type Color uint32

func RGB(r, g, b uint8) Color { return Color(r)<<16 | Color(g)<<8 | Color(b) }

func (c Color) RGB() (r, g, b uint8) { return uint8(c >> 16), uint8(c >> 8), uint8(c) }

// Framebuffer is one screen.
type Framebuffer struct {
	Width, Height int
	Stride        int // pixels from one row to the next
	Pix           []uint32
}

// New checks that pix holds height rows of pitchBytes.
func New(pix []uint32, width, height, pitchBytes int) (*Framebuffer, error) {
	if width <= 0 || height <= 0 || pitchBytes < width*4 || pitchBytes%4 != 0 {
		return nil, fmt.Errorf("gfx: bad mode %dx%d pitch %d", width, height, pitchBytes)
	}
	stride := pitchBytes / 4
	if len(pix) < stride*(height-1)+width {
		return nil, fmt.Errorf("gfx: %d pixels of memory for %dx%d pitch %d", len(pix), width, height, pitchBytes)
	}
	return &Framebuffer{Width: width, Height: height, Stride: stride, Pix: pix}, nil
}

// Words views device memory (4-byte aligned, as a BAR is) as pixels.
func Words(b []byte) []uint32 {
	if len(b) < 4 {
		return nil
	}
	return unsafe.Slice((*uint32)(unsafe.Pointer(&b[0])), len(b)/4)
}

func (f *Framebuffer) Bounds() image.Rectangle { return image.Rect(0, 0, f.Width, f.Height) }

func (f *Framebuffer) Set(x, y int, c Color) {
	if x >= 0 && y >= 0 && x < f.Width && y < f.Height {
		f.Pix[y*f.Stride+x] = uint32(c)
	}
}

func (f *Framebuffer) At(x, y int) Color { return Color(f.Pix[y*f.Stride+x]) }

// Fill paints r (clipped to the screen).
func (f *Framebuffer) Fill(r image.Rectangle, c Color) {
	r = r.Intersect(f.Bounds())
	for y := r.Min.Y; y < r.Max.Y; y++ {
		row := f.Pix[y*f.Stride+r.Min.X : y*f.Stride+r.Max.X]
		for i := range row {
			row[i] = uint32(c)
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
