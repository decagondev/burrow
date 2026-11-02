package gfx

import (
	"image"
	"strings"
	"testing"
)

// A screen whose rows have 8 pixels of padding, like a device whose pitch
// is not width*4 (PIT-27). The padding must never be drawn on.
const padColor = 0xDEADBEEF

func screen(t *testing.T, w, h int) *Framebuffer {
	t.Helper()
	stride := w + 8
	pix := make([]uint32, stride*h)
	for i := range pix {
		pix[i] = padColor
	}
	fb, err := New(pix, w, h, stride*4)
	if err != nil {
		t.Fatal(err)
	}
	return fb
}

func checkPadding(t *testing.T, fb *Framebuffer) {
	t.Helper()
	for y := 0; y < fb.Height; y++ {
		for x := fb.Width; x < fb.Stride; x++ {
			if fb.Pix[y*fb.Stride+x] != padColor {
				t.Fatalf("padding at (%d,%d) was drawn on", x, y)
			}
		}
	}
}

func TestNewRejectsBadModes(t *testing.T) {
	for _, c := range []struct{ n, w, h, pitch int }{
		{99, 10, 10, 40},   // too little memory
		{1000, 10, 10, 36}, // pitch below width*4
		{1000, 10, 10, 42}, // pitch not whole pixels
		{1000, 0, 10, 40},
	} {
		if _, err := New(make([]uint32, c.n), c.w, c.h, c.pitch); err == nil {
			t.Errorf("New(%+v) accepted", c)
		}
	}
}

func TestFillGradientClip(t *testing.T) {
	fb := screen(t, 64, 32)
	fb.Fill(image.Rect(-10, -10, 100, 100), RGB(1, 2, 3))
	if fb.At(63, 31) != RGB(1, 2, 3) || fb.At(0, 0) != RGB(1, 2, 3) {
		t.Error("Fill missed corners")
	}
	from, to := RGB(0x00, 0x7D, 0x9C), RGB(0x5D, 0xC9, 0xE2)
	fb.Gradient(image.Rect(0, 0, 64, 8), from, to)
	if fb.At(0, 3) != from || fb.At(63, 3) != to {
		t.Errorf("gradient ends: %06x .. %06x", fb.At(0, 3), fb.At(63, 3))
	}
	if r, _, _ := fb.At(32, 3).RGB(); r <= 0x00 || r >= 0x5D {
		t.Errorf("gradient middle red = %#x", r)
	}
	if fb.At(10, 8) != RGB(1, 2, 3) {
		t.Error("gradient spilled below its rectangle")
	}
	checkPadding(t, fb)
}

// Draw "Hi" at scale 1 and read the ink back as text.
func TestDrawTextMatchesFont(t *testing.T) {
	fb := screen(t, 2*CellW, CellH)
	fb.Fill(fb.Bounds(), 0)
	fb.DrawText(0, 0, "Hi", 0xFFFFFF, 1)
	var rows []string
	for y := 0; y < GlyphH; y++ {
		var b strings.Builder
		for x := 0; x < 2*CellW; x++ {
			if fb.At(x, y) != 0 {
				b.WriteByte('#')
			} else {
				b.WriteByte('.')
			}
		}
		rows = append(rows, b.String())
	}
	want := []string{
		"#...#...#...", "#...#.......", "#...#..##...", "#####...#...",
		"#...#...#...", "#...#...#...", "#...#..###..", "............",
	}
	if strings.Join(rows, "\n") != strings.Join(want, "\n") {
		t.Errorf("drew\n%s\nwant\n%s", strings.Join(rows, "\n"), strings.Join(want, "\n"))
	}
	checkPadding(t, fb)
}

func TestFontCoversPrintableASCII(t *testing.T) {
	for c := byte(0x20); c < 0x7F; c++ {
		src, ok := glyphSource[c]
		if !ok {
			t.Errorf("no glyph for %q", c)
			continue
		}
		rows := strings.Fields(src)
		if len(rows) != GlyphH {
			t.Errorf("%q has %d rows", c, len(rows))
		}
		for _, r := range rows {
			if len(r) != GlyphW || strings.Trim(r, "#.") != "" {
				t.Errorf("%q row %q", c, r)
			}
		}
	}
	if len(glyphSource) != 95 {
		t.Errorf("%d glyphs, want 95", len(glyphSource))
	}
}

func TestConsoleWrapsBackspacesScrolls(t *testing.T) {
	fb := screen(t, 4*CellW, 2*CellH)
	con := NewConsole(fb, fb.Bounds(), 1, 0xFFFFFF, 0x000010)
	if cols, rows := con.Size(); cols != 4 || rows != 2 {
		t.Fatalf("Size = %d x %d", cols, rows)
	}
	con.Write([]byte("abcde")) // wraps after 4
	if col, row := con.Cursor(); col != 1 || row != 1 {
		t.Errorf("after wrap: cursor %d,%d", col, row)
	}
	con.Write([]byte("x\b \b")) // the line editor's erase
	if col, _ := con.Cursor(); col != 1 {
		t.Errorf("after erase: col %d", col)
	}
	inkAt := func(cellCol, cellRow int) bool { // the left stem of most letters
		for y := 0; y < GlyphH; y++ {
			for x := 0; x < GlyphW; x++ {
				if fb.At(cellCol*CellW+x, cellRow*CellH+y) == 0xFFFFFF {
					return true
				}
			}
		}
		return false
	}
	if !inkAt(0, 0) || !inkAt(0, 1) || inkAt(1, 1) {
		t.Error("cells hold the wrong ink before scrolling")
	}
	con.Write([]byte("\n")) // scroll: row 1 ("e") moves to row 0, row 1 clears
	if !inkAt(0, 0) || inkAt(0, 1) || inkAt(1, 0) {
		t.Error("scroll did not move row 1 up and clear it")
	}
	if fb.At(3*CellW, CellH+1) != 0x000010 {
		t.Error("the cleared row is not the background colour")
	}
	checkPadding(t, fb)
}
