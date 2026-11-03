package gfx

// vt.go - the VT100/ANSI subset the console understands (W13-S02): what
// busybox vi, ash's line editor and small editors like kilo send. ESC [
// sequences: cursor position and moves (H f A B C D G d), erase in screen
// and line (J K), insert and delete lines (L M), colours and attributes
// (m: 0 1 7 22 27 30-37 39 40-47 49 90-97 100-107), the scrolling region
// (r), cursor visibility (?25) and the alternate screen (?1049, ?47).
// ESC 7 / ESC 8 save and restore the cursor, ESC M is a reverse index,
// ESC c resets. Everything else is swallowed whole, never printed: unknown
// CSI finals, charset selection (ESC ( B), OSC strings (ESC ] ... BEL).
// It never answers queries (ESC[6n): the console has no way to type back.

const (
	vtGround = iota
	vtEscape
	vtCSI
	vtCharset // ESC ( or ESC ): one more byte
	vtOSC     // until BEL or ESC \
	maxParams = 16
)

type vtParser struct {
	state   int
	params  []int
	cur     int
	have    bool // cur holds digits
	private bool // ESC [ ?
}

func (v *vtParser) start() { *v = vtParser{state: vtEscape, params: v.params[:0]} }

// ansi is the 8-colour palette, then its bright half (xterm's defaults).
var ansi = [16]Color{
	0x000000, 0xCD0000, 0x00CD00, 0xCDCD00, 0x0000EE, 0xCD00CD, 0x00CDCD, 0xE5E5E5,
	0x7F7F7F, 0xFF0000, 0x00FF00, 0xFFFF00, 0x5C5CFF, 0xFF00FF, 0x00FFFF, 0xFFFFFF,
}

// brighter is the bold version of a colour: the bright palette entry, or white.
func brighter(c Color) Color {
	for i := 0; i < 8; i++ {
		if ansi[i] == c {
			return ansi[i+8]
		}
	}
	return 0xFFFFFF
}

func (c *Console) escape(b byte) {
	v := &c.vt
	switch v.state {
	case vtEscape:
		v.state = vtGround
		switch b {
		case '[':
			v.state = vtCSI
		case '(', ')':
			v.state = vtCharset
		case ']':
			v.state = vtOSC
		case '7':
			c.savedCol, c.savedRw = c.col, c.row
		case '8':
			c.col, c.row = c.savedCol, c.savedRw
		case 'M': // reverse index
			if c.row == c.top {
				c.scrollDown(c.top, c.bottom, 1)
			} else if c.row > 0 {
				c.row--
			}
		case 'c':
			c.reset()
		}
	case vtCharset:
		v.state = vtGround
	case vtOSC:
		if b == 0x07 || b == '\\' {
			v.state = vtGround
		}
	case vtCSI:
		switch {
		case b >= '0' && b <= '9':
			v.cur = min(v.cur*10+int(b-'0'), 9999)
			v.have = true
		case b == ';':
			v.push()
		case b == '?':
			v.private = true
		case b >= 0x40 && b <= 0x7E:
			v.push()
			v.state = vtGround
			c.csi(b, v.params, v.private)
		case b < 0x20: // a control character inside a sequence: run it, keep parsing
			v.state = vtGround
			c.put(b)
			v.state = vtCSI
		}
	}
}

func (v *vtParser) push() {
	if len(v.params) < maxParams {
		p := v.cur
		if !v.have {
			p = -1 // omitted: the final's default applies
		}
		v.params = append(v.params, p)
	}
	v.cur, v.have = 0, false
}

// arg is parameter i, or def when it is missing or 0 (for counts).
func arg(p []int, i, def int) int {
	if i >= len(p) || p[i] <= 0 {
		return def
	}
	return p[i]
}

func clamp(v, lo, hi int) int { return max(lo, min(v, hi)) }

func (c *Console) csi(final byte, p []int, private bool) {
	if private {
		c.mode(final, p)
		return
	}
	col := min(c.col, c.cols-1)
	switch final {
	case 'H', 'f':
		c.row, c.col = clamp(arg(p, 0, 1)-1, 0, c.rows-1), clamp(arg(p, 1, 1)-1, 0, c.cols-1)
	case 'A':
		c.row = clamp(c.row-arg(p, 0, 1), 0, c.rows-1)
	case 'B':
		c.row = clamp(c.row+arg(p, 0, 1), 0, c.rows-1)
	case 'C':
		c.col = clamp(col+arg(p, 0, 1), 0, c.cols-1)
	case 'D':
		c.col = clamp(col-arg(p, 0, 1), 0, c.cols-1)
	case 'G':
		c.col = clamp(arg(p, 0, 1)-1, 0, c.cols-1)
	case 'd':
		c.row = clamp(arg(p, 0, 1)-1, 0, c.rows-1)
	case 'J':
		at := c.row*c.cols + col
		switch arg(p, 0, 0) {
		case 0:
			c.erase(at, len(c.cells))
		case 1:
			c.erase(0, at+1)
		default:
			c.erase(0, len(c.cells))
		}
	case 'K':
		start := c.row * c.cols
		switch arg(p, 0, 0) {
		case 0:
			c.erase(start+col, start+c.cols)
		case 1:
			c.erase(start, start+col+1)
		default:
			c.erase(start, start+c.cols)
		}
	case 'L':
		if c.row >= c.top && c.row <= c.bottom {
			c.scrollDown(c.row, c.bottom, arg(p, 0, 1))
		}
	case 'M':
		if c.row >= c.top && c.row <= c.bottom {
			c.scrollUp(c.row, c.bottom, arg(p, 0, 1))
		}
	case 'm':
		c.sgr(p)
	case 'r':
		top, bottom := arg(p, 0, 1)-1, arg(p, 1, c.rows)-1
		if top < bottom && bottom < c.rows {
			c.top, c.bottom = top, bottom
			c.row, c.col = 0, 0
		}
	}
}

// mode handles ESC [ ? ... h / l: the cursor and the alternate screen.
func (c *Console) mode(final byte, p []int) {
	on := final == 'h'
	if final != 'h' && final != 'l' {
		return
	}
	for _, m := range p {
		switch m {
		case 25:
			c.cursorOn = on
		case 1049, 47, 1047:
			c.altScreen(on)
		}
	}
}

func (c *Console) altScreen(on bool) {
	switch {
	case on && c.alt == nil:
		c.alt = &screenCopy{append([]cell(nil), c.cells...), c.col, c.row}
		c.erase(0, len(c.cells))
	case !on && c.alt != nil:
		copy(c.cells, c.alt.cells)
		c.col, c.row = c.alt.col, c.alt.row
		c.alt = nil
		c.redraw()
	}
}

func (c *Console) sgr(p []int) {
	if len(p) == 0 {
		p = []int{0}
	}
	for _, a := range p {
		switch {
		case a <= 0:
			c.fg, c.bg, c.bold, c.reverse = c.defFG, c.defBG, false, false
		case a == 1:
			c.bold = true
		case a == 22:
			c.bold = false
		case a == 7:
			c.reverse = true
		case a == 27:
			c.reverse = false
		case a >= 30 && a <= 37:
			c.fg = ansi[a-30]
		case a == 39:
			c.fg = c.defFG
		case a >= 40 && a <= 47:
			c.bg = ansi[a-40]
		case a == 49:
			c.bg = c.defBG
		case a >= 90 && a <= 97:
			c.fg = ansi[a-90+8]
		case a >= 100 && a <= 107:
			c.bg = ansi[a-100+8]
		}
	}
}

func (c *Console) reset() {
	c.fg, c.bg, c.bold, c.reverse, c.cursorOn = c.defFG, c.defBG, false, false, true
	c.top, c.bottom, c.col, c.row, c.alt = 0, c.rows-1, 0, 0, nil
	c.erase(0, len(c.cells))
}
