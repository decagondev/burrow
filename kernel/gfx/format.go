// format.go - how a pixel is laid out in memory (W21-S03): its size in
// bytes and where its red, green and blue fields sit, as the firmware's
// mode says (boot_info, VBE's ModeInfoBlock). Pure: packs and unpacks
// colours and never touches memory itself. Docs:
// docs/04-plan/waves/WAVE-21-framebuffer.md.
package gfx

import "fmt"

// Field is one colour channel: Size bits starting at bit Pos.
type Field struct{ Size, Pos uint8 }

// Format is a direct-colour pixel of Bytes bytes, stored little-endian.
type Format struct {
	Bytes            int
	Red, Green, Blue Field
}

// XRGB8888 is QEMU's and the BGA's 32 bpp layout, 0x00RRGGBB; Color is the
// same, so packing is free for it.
var XRGB8888 = Format{Bytes: 4, Red: Field{8, 16}, Green: Field{8, 8}, Blue: Field{8, 0}}

// FormatFor builds the format of a bpp-bit mode with these fields.
func FormatFor(bpp int, red, green, blue Field) (Format, error) {
	f := Format{Bytes: (bpp + 7) / 8, Red: red, Green: green, Blue: blue}
	return f, f.check()
}

func (f Format) check() error {
	if f.Bytes < 2 || f.Bytes > 4 {
		return fmt.Errorf("gfx: %d bytes per pixel", f.Bytes)
	}
	var seen uint32
	for _, c := range []Field{f.Red, f.Green, f.Blue} {
		if c.Size == 0 || c.Size > 8 || int(c.Pos)+int(c.Size) > 8*f.Bytes {
			return fmt.Errorf("gfx: colour field %d@%d in a %d-byte pixel", c.Size, c.Pos, f.Bytes)
		}
		m := c.mask()
		if seen&m != 0 {
			return fmt.Errorf("gfx: colour fields overlap at %d@%d", c.Size, c.Pos)
		}
		seen |= m
	}
	return nil
}

func (c Field) mask() uint32 { return (1<<c.Size - 1) << c.Pos }

// pack keeps the top Size bits of an 8-bit channel, at Pos.
func (c Field) pack(v uint8) uint32 { return uint32(v>>(8-c.Size)) << c.Pos }

// unpack widens the field back to 8 bits, repeating its top bits below so
// that full intensity stays 0xFF.
func (c Field) unpack(p uint32) uint8 {
	v := (p >> c.Pos) & (1<<c.Size - 1)
	out := v << (8 - c.Size)
	for s := 8 - int(c.Size); s > 0; s -= int(c.Size) {
		if s >= int(c.Size) {
			out |= v << (s - int(c.Size))
		} else {
			out |= v >> (int(c.Size) - s)
		}
	}
	return uint8(out)
}

// Pack is c as this format stores it.
func (f Format) Pack(c Color) uint32 {
	if f == XRGB8888 {
		return uint32(c)
	}
	r, g, b := c.RGB()
	return f.Red.pack(r) | f.Green.pack(g) | f.Blue.pack(b)
}

// Unpack is the colour a stored pixel shows.
func (f Format) Unpack(p uint32) Color {
	if f == XRGB8888 {
		return Color(p & 0xFFFFFF)
	}
	return RGB(f.Red.unpack(p), f.Green.unpack(p), f.Blue.unpack(p))
}
