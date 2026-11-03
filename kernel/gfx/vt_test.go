package gfx

import (
	"strings"
	"testing"
)

const (
	testFG Color = 0xE6E6E6
	testBG Color = 0x1E1E28
)

func term(t *testing.T, cols, rows int) (*Console, *Framebuffer) {
	t.Helper()
	fb := screen(t, cols*CellW, rows*CellH)
	return NewConsole(fb, fb.Bounds(), 1, testFG, testBG), fb
}

// lines is the screen as text, trailing blanks trimmed.
func lines(c *Console) []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]string, c.rows)
	for r := 0; r < c.rows; r++ {
		b := make([]byte, c.cols)
		for col := 0; col < c.cols; col++ {
			b[col] = c.cells[r*c.cols+col].ch
		}
		out[r] = strings.TrimRight(string(b), " ")
	}
	return out
}

func want(t *testing.T, c *Console, rows ...string) {
	t.Helper()
	got := lines(c)
	for i, w := range rows {
		if got[i] != w {
			t.Errorf("row %d = %q, want %q (screen %q)", i, got[i], w, got)
		}
	}
}

func TestCursorPositioningAndErase(t *testing.T) {
	c, fb := term(t, 10, 4)
	c.Write([]byte("aaaaaaaaaa\nbbbbbbbbbb\ncccccccccc\ndddddddddd"))
	c.Write([]byte("\x1b[2;3HX"))       // CUP is 1-based
	c.Write([]byte("\x1b[3;5H\x1b[K"))  // erase to end of line
	c.Write([]byte("\x1b[1;4H\x1b[1K")) // erase to start of line, cursor included
	want(t, c, "    aaaaaa", "bbXbbbbbbb", "cccc", "dddddddddd")
	c.Write([]byte("\x1b[4;6H\x1b[J")) // erase to end of screen
	want(t, c, "    aaaaaa", "bbXbbbbbbb", "cccc", "ddddd")
	c.Write([]byte("\x1b[2J\x1b[HZ"))
	want(t, c, "Z", "", "", "")
	if col, row := c.Cursor(); col != 1 || row != 0 {
		t.Errorf("cursor %d,%d after ESC[H and one character", col, row)
	}
	c.Write([]byte("\x1b[3B\x1b[2C\x1b[1Aq\x1b[99D\x1b[99Ar")) // relative moves clamp
	want(t, c, "r", "", "   q")                                // Z left the cursor at column 1: down 3 (clamped to row 3), right 2, up 1
	c.Write([]byte("\x1b[2G\x1b[3dk"))                         // CHA and VPA
	want(t, c, "r", "", " k q")
	checkPadding(t, fb)
}

func TestColoursAndReverse(t *testing.T) {
	c, fb := term(t, 6, 2)
	cellBG := func(col, row int) Color { return fb.At(col*CellW+GlyphW, row*CellH) } // a pixel no glyph inks
	c.Write([]byte("\x1b[7mR\x1b[0mN\x1b[41mB\x1b[m "))
	if cellBG(0, 0) != testFG || cellBG(1, 0) != testBG || cellBG(2, 0) != ansi[1] || cellBG(3, 0) != testBG {
		t.Errorf("backgrounds %06x %06x %06x %06x", cellBG(0, 0), cellBG(1, 0), cellBG(2, 0), cellBG(3, 0))
	}
	c.Write([]byte("\x1b[32;44m\x1b[K")) // erase uses the current background
	if cellBG(5, 0) != ansi[4] {
		t.Errorf("erased cell %06x, want blue", cellBG(5, 0))
	}
	c.mu.Lock()
	fg := c.fg
	c.mu.Unlock()
	if fg != ansi[2] {
		t.Errorf("foreground %06x, want green", fg)
	}
}

func TestScrollRegionAndIndex(t *testing.T) {
	c, _ := term(t, 5, 5)
	c.Write([]byte("top\nr1\nr2\nr3\nbot"))
	c.Write([]byte("\x1b[2;4r"))      // rows 2-4 scroll, cursor home
	c.Write([]byte("\x1b[4;1H\nnew")) // a line feed at the region's bottom scrolls only the region
	want(t, c, "top", "r2", "r3", "new", "bot")
	c.Write([]byte("\x1b[2;1H\x1bMup")) // reverse index at the region's top scrolls it down
	want(t, c, "top", "up", "r2", "r3", "bot")
	c.Write([]byte("\x1b[2;1H\x1b[L")) // insert a line: the region's last row falls off
	want(t, c, "top", "", "up", "r2", "bot")
	c.Write([]byte("\x1b[2;1H\x1b[2M")) // delete two lines
	want(t, c, "top", "r2", "", "", "bot")
	c.Write([]byte("\x1b[r\x1b[5;1H\nend")) // region reset: the whole screen scrolls again
	want(t, c, "r2", "", "", "bot", "end")
}

// vi switches to the alternate screen and back; the shell's lines return.
func TestAlternateScreenAndSavedCursor(t *testing.T) {
	c, _ := term(t, 8, 3)
	c.Write([]byte("$ vi\n"))
	c.Write([]byte("\x1b[?1049h\x1b[H\x1b[J~\n~\nedit"))
	want(t, c, "~", "~", "edit")
	c.Write([]byte("\x1b[?1049l"))
	want(t, c, "$ vi", "", "")
	if col, row := c.Cursor(); col != 0 || row != 1 {
		t.Errorf("cursor %d,%d after leaving the alternate screen", col, row)
	}
	c.Write([]byte("ab\x1b7\x1b[3;5Hx\x1b8c")) // ESC 7 / ESC 8 save and restore
	want(t, c, "$ vi", "abc", "    x")
	c.Write([]byte("\x1b(B\x1b[?2004h\x1b[5n\x1b]0;title\x07ok")) // swallowed: charset, unknown modes, OSC
	want(t, c, "$ vi", "abcok", "    x")
}

// The cursor is a line under the cell, in the rows no glyph inks; hidden
// with ESC[?25l, it is not drawn at all.
func TestCursorIsDrawn(t *testing.T) {
	c, fb := term(t, 4, 2)
	under := func(col, row int) Color { return fb.At(col*CellW+1, row*CellH+CellH-1) }
	c.Write([]byte("ab"))
	if under(2, 0) != testFG || under(1, 0) != testBG {
		t.Errorf("cursor line %06x under the cursor, %06x beside it", under(2, 0), under(1, 0))
	}
	c.Write([]byte("\x1b[?25l"))
	if under(2, 0) != testBG {
		t.Error("a hidden cursor is still drawn")
	}
	c.Write([]byte("\x1b[?25h\x1b[2;1H"))
	if under(0, 1) != testFG || under(2, 0) != testBG {
		t.Error("the cursor did not move, or left its old line behind")
	}
}
