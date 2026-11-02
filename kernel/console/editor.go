// Package console is the kernel's interactive line console (E06.F04): bytes
// from every input driver (PS/2 keyboard, COM1) go through a line editor,
// finished lines run a command. It knows nothing about hardware: input is a
// channel, output an io.Writer, so it runs unchanged in host tests.
package console

// MaxLine is the longest line the editor keeps; further bytes are dropped.
const MaxLine = 256

const (
	ctrlC     = 0x03
	backspace = 0x08
	ctrlU     = 0x15
	escape    = 0x1B
	del       = 0x7F
)

// Editor is a minimal canonical-mode line discipline: printable bytes echo,
// backspace/DEL erase, ^U kills the line, ^C abandons it, CR or LF ends it
// (CR LF counts once). Escape sequences (arrow keys) are swallowed.
type Editor struct {
	buf    []byte
	lastCR bool
	esc    int // 0 none, 1 after ESC, 2 inside "ESC ["
}

// Feed takes one input byte and returns what to echo, and the line when one
// is finished (done; an abandoned line is "" with done set).
func (e *Editor) Feed(b byte) (echo []byte, line string, done bool) {
	afterCR := e.lastCR
	e.lastCR = b == '\r'
	switch {
	case e.esc == 1:
		e.esc = 0
		if b == '[' {
			e.esc = 2
		}
		return nil, "", false
	case e.esc == 2:
		if b >= 0x40 && b <= 0x7E { // final byte of a CSI sequence
			e.esc = 0
		}
		return nil, "", false
	}
	switch {
	case b == escape:
		e.esc = 1
	case b == '\n' && afterCR:
	case b == '\r' || b == '\n':
		line = string(e.buf)
		e.buf = e.buf[:0]
		return []byte("\n"), line, true
	case b == ctrlC:
		e.buf = e.buf[:0]
		return []byte("^C\n"), "", true
	case b == backspace || b == del:
		if len(e.buf) > 0 {
			e.buf = e.buf[:len(e.buf)-1]
			return []byte("\b \b"), "", false
		}
	case b == ctrlU:
		for range e.buf {
			echo = append(echo, "\b \b"...)
		}
		e.buf = e.buf[:0]
		return echo, "", false
	case b >= 0x20 && b < del:
		if len(e.buf) < MaxLine {
			e.buf = append(e.buf, b)
			return []byte{b}, "", false
		}
	}
	return nil, "", false
}
