package gfx

import (
	"encoding/binary"
	"image"
	"testing"
)

var (
	bgr8888 = Format{Bytes: 4, Red: Field{8, 0}, Green: Field{8, 8}, Blue: Field{8, 16}}
	rgb565  = Format{Bytes: 2, Red: Field{5, 11}, Green: Field{6, 5}, Blue: Field{5, 0}}
	rgb888  = Format{Bytes: 3, Red: Field{8, 16}, Green: Field{8, 8}, Blue: Field{8, 0}}
)

const padByte = 0xA5

// odd builds a w x h screen in format f whose rows carry 13 bytes of padding
// beyond the pixels (a pitch no program would compute from the width).
func odd(t *testing.T, w, h int, f Format) *Framebuffer {
	t.Helper()
	pitch := w*f.Bytes + 13
	if f.Bytes == 4 {
		pitch += 3 // 4-byte pixels need a whole number of words a row
	}
	mem := make([]byte, pitch*h)
	for i := range mem {
		mem[i] = padByte
	}
	fb, err := New(mem, w, h, pitch, f)
	if err != nil {
		t.Fatal(err)
	}
	return fb
}

func checkOddPadding(t *testing.T, fb *Framebuffer) {
	t.Helper()
	for y := 0; y < fb.Height; y++ {
		for i := fb.Width * fb.Format.Bytes; i < fb.Pitch && y*fb.Pitch+i < len(fb.Mem); i++ {
			if fb.Mem[y*fb.Pitch+i] != padByte {
				t.Fatalf("padding byte %d of row %d was drawn on", i, y)
			}
		}
	}
}

// W21-S03: a 1000x700 screen, padded, in each layout; a pattern drawn by
// colour reads back as the bytes the layout says, and the padding stays.
func TestFormatsDrawTheirOwnBytes(t *testing.T) {
	for _, c := range []struct {
		name string
		f    Format
		want []byte // red, green, blue, then pure red, as stored (little-endian)
	}{
		{"xrgb8888", XRGB8888, []byte{0x00, 0x00, 0xFF, 0x00}},
		{"bgr8888", bgr8888, []byte{0xFF, 0x00, 0x00, 0x00}},
		{"rgb565", rgb565, []byte{0x00, 0xF8}},
		{"rgb888", rgb888, []byte{0x00, 0x00, 0xFF}},
	} {
		t.Run(c.name, func(t *testing.T) {
			fb := odd(t, 1000, 700, c.f)
			fb.Fill(fb.Bounds(), RGB(0, 0, 0))
			fb.Fill(image.Rect(990, 690, 1000, 700), RGB(0xFF, 0, 0)) // the far corner
			off := 699*fb.Pitch + 999*c.f.Bytes
			if got := fb.Mem[off : off+c.f.Bytes]; string(got) != string(c.want) {
				t.Errorf("pure red stored as % x, want % x", got, c.want)
			}
			if fb.At(999, 699) != RGB(0xFF, 0, 0) || fb.At(989, 699) != RGB(0, 0, 0) {
				t.Errorf("At: %06x and %06x", fb.At(999, 699), fb.At(989, 699))
			}
			fb.DrawText(900, 600, "Hi", RGB(0xFF, 0xFF, 0xFF), 2)
			checkOddPadding(t, fb)
		})
	}
}

func TestFormatPackUnpack(t *testing.T) {
	if XRGB8888.Pack(0x123456) != 0x123456 {
		t.Error("XRGB8888 must pack for free")
	}
	if p := rgb565.Pack(RGB(0xFF, 0xFF, 0xFF)); p != 0xFFFF {
		t.Errorf("white in rgb565 = %#x", p)
	}
	if c := rgb565.Unpack(0xFFFF); c != RGB(0xFF, 0xFF, 0xFF) {
		t.Errorf("rgb565 white unpacks to %06x", c)
	}
	if c := rgb565.Unpack(uint32(binary.LittleEndian.Uint16([]byte{0xE0, 0x07}))); c != RGB(0, 0xFF, 0) {
		t.Errorf("rgb565 green unpacks to %06x", c)
	}
	for _, bad := range []Format{
		{Bytes: 1, Red: Field{3, 5}, Green: Field{3, 2}, Blue: Field{2, 0}},
		{Bytes: 2, Red: Field{5, 12}, Green: Field{6, 5}, Blue: Field{5, 0}},  // red past 16 bits
		{Bytes: 4, Red: Field{8, 16}, Green: Field{8, 12}, Blue: Field{8, 0}}, // overlap
		{Bytes: 4, Red: Field{0, 16}, Green: Field{8, 8}, Blue: Field{8, 0}},
	} {
		if err := bad.check(); err == nil {
			t.Errorf("format %+v accepted", bad)
		}
	}
}

// The console scrolls by copying rows of bytes: the same text in RGB565 on
// an odd, padded screen.
func TestConsoleScrollsInAnyFormat(t *testing.T) {
	fb := odd(t, 4*CellW+3, 2*CellH, rgb565)
	con := NewConsole(fb, fb.Bounds(), 1, RGB(0xFF, 0xFF, 0xFF), RGB(0, 0, 0))
	con.Write([]byte("ab\ncd\nef"))
	white := func(col, row int) bool {
		for y := 0; y < GlyphH; y++ {
			for x := 0; x < GlyphW; x++ {
				if fb.At(col*CellW+x, row*CellH+y) == RGB(0xFF, 0xFF, 0xFF) {
					return true
				}
			}
		}
		return false
	}
	if !white(0, 0) || !white(0, 1) { // "cd" scrolled to the top, "ef" below
		t.Error("scrolled text missing")
	}
	checkOddPadding(t, fb)
}
