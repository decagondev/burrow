package gfx

import (
	"image"
	"sync"
)

// Console is a text terminal in a rectangle of the framebuffer: an
// io.Writer that understands \n, \r, \b and \t, wraps long lines and
// scrolls. The kernel tees the serial console into it.
type Console struct {
	mu         sync.Mutex
	fb         *Framebuffer
	area       image.Rectangle
	scale      int
	fg, bg     Color
	cols, rows int
	col, row   int
}

// NewConsole clears area and starts at its top-left cell.
func NewConsole(fb *Framebuffer, area image.Rectangle, scale int, fg, bg Color) *Console {
	area = area.Intersect(fb.Bounds())
	c := &Console{fb: fb, area: area, scale: scale, fg: fg, bg: bg,
		cols: area.Dx() / (CellW * scale), rows: area.Dy() / (CellH * scale)}
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
	for _, b := range p {
		c.put(b)
	}
	return len(p), nil
}

func (c *Console) put(b byte) {
	switch b {
	case '\n':
		c.newline()
	case '\r':
		c.col = 0
	case '\b':
		if c.col > 0 {
			c.col--
		}
	case '\t':
		for c.put(' '); c.col%8 != 0; {
			c.put(' ')
		}
	default:
		if b < 0x20 || b == 0x7F {
			return
		}
		if c.col == c.cols {
			c.newline()
		}
		cw, ch := CellW*c.scale, CellH*c.scale
		c.fb.DrawGlyph(c.area.Min.X+c.col*cw, c.area.Min.Y+c.row*ch, b, c.fg, int64(c.bg), c.scale)
		c.col++
	}
}

func (c *Console) newline() {
	c.col = 0
	if c.row < c.rows-1 {
		c.row++
		return
	}
	c.scroll()
}

// scroll moves every text row up one and clears the last.
func (c *Console) scroll() {
	ch := CellH * c.scale
	fb, a := c.fb, c.area
	for y := a.Min.Y; y < a.Min.Y+(c.rows-1)*ch; y++ {
		copy(fb.Pix[y*fb.Stride+a.Min.X:y*fb.Stride+a.Max.X], fb.Pix[(y+ch)*fb.Stride+a.Min.X:(y+ch)*fb.Stride+a.Max.X])
	}
	fb.Fill(image.Rect(a.Min.X, a.Min.Y+(c.rows-1)*ch, a.Max.X, a.Min.Y+c.rows*ch), c.bg)
}
