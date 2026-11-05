package gfx

import (
	"image"
	"sync"
)

// Console is a text terminal in a rectangle of the framebuffer: an
// io.Writer that understands \n, \r, \b and \t, wraps long lines and
// scrolls, and (W13-S02) the VT100 subset editors and shells use (vt.go).
// It keeps a grid of cells, so it can redraw, show a cursor and swap to an
// alternate screen. The kernel tees the serial console into it. It never
// draws outside its area.
type Console struct {
	mu         sync.Mutex
	fb         *Framebuffer
	area       image.Rectangle
	scale      int
	defFG      Color // the colours SGR 0 returns to
	defBG      Color
	cols, rows int
	col, row   int    // col == cols: wrap before the next character
	cells      []cell // rows * cols

	// VT100 state (vt.go)
	fg, bg            Color
	reverse, bold     bool
	top, bottom       int // scrolling region, rows inclusive
	cursorOn          bool
	savedCol, savedRw int
	alt               *screenCopy // the main screen while the alternate one shows
	vt                vtParser
}

type cell struct {
	ch     byte
	fg, bg Color
}

type screenCopy struct {
	cells    []cell
	col, row int
}

// NewConsole clears area and starts at its top-left cell.
func NewConsole(fb *Framebuffer, area image.Rectangle, scale int, fg, bg Color) *Console {
	area = area.Intersect(fb.Bounds())
	c := &Console{fb: fb, area: area, scale: scale, defFG: fg, defBG: bg, fg: fg, bg: bg,
		cols: area.Dx() / (CellW * scale), rows: area.Dy() / (CellH * scale), cursorOn: true}
	c.cells = make([]cell, c.cols*c.rows)
	c.top, c.bottom = 0, c.rows-1
	c.clearCells(0, len(c.cells))
	fb.Fill(area, bg)
	return c
}

// Size is the console in character cells.
func (c *Console) Size() (cols, rows int) { return c.cols, c.rows }

// Cursor is the cell the next character goes to.
func (c *Console) Cursor() (col, row int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.col, c.row
}

func (c *Console) Write(p []byte) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.cols == 0 || c.rows == 0 {
		return len(p), nil
	}
	c.drawCursor(false)
	for _, b := range p {
		c.put(b)
	}
	c.drawCursor(true)
	return len(p), nil
}

func (c *Console) put(b byte) {
	if c.vt.state != vtGround {
		c.escape(b)
		return
	}
	switch b {
	case 0x1B:
		c.vt.start()
	case '\n':
		c.col = 0
		c.lineFeed()
	case '\r':
		c.col = 0
	case '\b':
		c.col = min(c.col, c.cols-1)
		if c.col > 0 {
			c.col--
		}
	case '\t':
		for c.putChar(' '); c.col%8 != 0 && c.col < c.cols; {
			c.putChar(' ')
		}
	default:
		if b >= 0x20 && b != 0x7F {
			c.putChar(b)
		}
	}
}

func (c *Console) putChar(b byte) {
	if c.col == c.cols {
		c.col = 0
		c.lineFeed()
	}
	fg, bg := c.colours()
	c.cells[c.row*c.cols+c.col] = cell{b, fg, bg}
	c.drawCell(c.col, c.row)
	c.col++
}

// colours are what the next character is drawn in (bold and reverse applied).
func (c *Console) colours() (Color, Color) {
	fg, bg := c.fg, c.bg
	if c.bold {
		fg = brighter(fg)
	}
	if c.reverse {
		fg, bg = bg, fg
	}
	return fg, bg
}

// lineFeed moves down a row, scrolling the region at its bottom.
func (c *Console) lineFeed() {
	switch {
	case c.row == c.bottom:
		c.scrollUp(c.top, c.bottom, 1)
	case c.row < c.rows-1:
		c.row++
	}
}

func (c *Console) drawCell(col, row int) {
	cl := c.cells[row*c.cols+col]
	cw, ch := CellW*c.scale, CellH*c.scale
	c.fb.DrawGlyph(c.area.Min.X+col*cw, c.area.Min.Y+row*ch, cl.ch, cl.fg, int64(cl.bg), c.scale)
}

// drawCursor draws (or, on = false, removes) the line under the cursor's
// cell, in the cell's bottom pixel rows that no glyph inks.
func (c *Console) drawCursor(on bool) {
	col := min(c.col, c.cols-1)
	cl := c.cells[c.row*c.cols+col]
	colour := cl.bg
	if on && c.cursorOn {
		colour = cl.fg
	}
	cw, ch := CellW*c.scale, CellH*c.scale
	x, y := c.area.Min.X+col*cw, c.area.Min.Y+(c.row+1)*ch-c.scale
	c.fb.Fill(image.Rect(x, y, x+cw, y+c.scale), colour)
}

// clearCells blanks cells [from, to) in the current background (not drawn).
func (c *Console) clearCells(from, to int) {
	for i := from; i < to; i++ {
		c.cells[i] = cell{' ', c.fg, c.bg}
	}
}

// erase blanks cells [from, to) and draws them.
func (c *Console) erase(from, to int) {
	from, to = max(from, 0), min(to, len(c.cells))
	if from >= to {
		return
	}
	c.clearCells(from, to)
	cw, ch := CellW*c.scale, CellH*c.scale
	for i := from; i < to; {
		row, col := i/c.cols, i%c.cols
		end := min(to, (row+1)*c.cols) // one span per row
		y := c.area.Min.Y + row*ch
		c.fb.Fill(image.Rect(c.area.Min.X+col*cw, y, c.area.Min.X+(end-row*c.cols)*cw, y+ch), c.bg)
		i = end
	}
}

// scrollUp moves rows top+n..bottom up n rows and blanks the n below.
func (c *Console) scrollUp(top, bottom, n int) {
	n = min(n, bottom-top+1)
	copy(c.cells[top*c.cols:], c.cells[(top+n)*c.cols:(bottom+1)*c.cols])
	c.moveRows(top+n, top, bottom-top+1-n)
	c.erase((bottom+1-n)*c.cols, (bottom+1)*c.cols)
}

// scrollDown moves rows top..bottom-n down n rows and blanks the n above.
func (c *Console) scrollDown(top, bottom, n int) {
	n = min(n, bottom-top+1)
	copy(c.cells[(top+n)*c.cols:(bottom+1)*c.cols], c.cells[top*c.cols:(bottom+1-n)*c.cols])
	c.moveRows(top, top+n, bottom-top+1-n)
	c.erase(top*c.cols, (top+n)*c.cols)
}

// moveRows copies count text rows of pixels from row from to row to.
func (c *Console) moveRows(from, to, count int) {
	if count <= 0 {
		return
	}
	ch := CellH * c.scale
	fb, a := c.fb, c.area
	lines := count * ch
	for i := 0; i < lines; i++ {
		y := i
		if to > from { // moving down: copy from the bottom so nothing is overwritten first
			y = lines - 1 - i
		}
		dst, src := (a.Min.Y+to*ch+y)*fb.Pitch, (a.Min.Y+from*ch+y)*fb.Pitch
		x0, x1 := a.Min.X*fb.Format.Bytes, a.Max.X*fb.Format.Bytes
		copy(fb.Mem[dst+x0:dst+x1], fb.Mem[src+x0:src+x1])
	}
}

// redraw paints every cell (after the alternate screen swaps).
func (c *Console) redraw() {
	for r := 0; r < c.rows; r++ {
		for col := 0; col < c.cols; col++ {
			c.drawCell(col, r)
		}
	}
}
