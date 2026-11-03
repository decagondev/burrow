package tty

// control.go - a terminal's settings and who it signals: termios (TCGETS,
// TCSETS*), the foreground process group, the window size (a new size is
// SIGWINCH, W16-S03) and FIONREAD. Split from tty.go, which keeps the input
// and output paths. Docs: docs/02-architecture/POSIX_COVERAGE.md.

// Termios and SetTermios are TCGETS and TCSETS (flush: TCSETSF drops input).
func (t *TTY) Termios() Termios { t.mu.Lock(); defer t.mu.Unlock(); return t.termios }

func (t *TTY) SetTermios(tm Termios, flush bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	wasCanon := t.termios.Lflag&ICANON != 0
	t.termios = tm
	if flush {
		t.line, t.ready = t.line[:0], t.ready[:0]
	}
	if wasCanon && tm.Lflag&ICANON == 0 { // to raw: the half-typed line is input now
		t.ready = append(t.ready, t.line...)
		t.line = t.line[:0]
	}
	t.notify()
}

// Foreground is the process group ^C goes to (TIOCGPGRP/TIOCSPGRP).
func (t *TTY) Foreground() int        { t.mu.Lock(); defer t.mu.Unlock(); return t.fg }
func (t *TTY) SetForeground(pgrp int) { t.mu.Lock(); t.fg = pgrp; t.mu.Unlock() }

// Size is TIOCGWINSZ.
func (t *TTY) Size() (rows, cols uint16) { t.mu.Lock(); defer t.mu.Unlock(); return t.rows, t.cols }

// SetSize is TIOCSWINSZ: a new size is SIGWINCH for the foreground group.
func (t *TTY) SetSize(rows, cols uint16) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if rows == t.rows && cols == t.cols {
		return
	}
	t.rows, t.cols = rows, cols
	t.signalFG(SIGWINCH)
}

// signalFG sends sig to the foreground group (t.mu held).
func (t *TTY) signalFG(sig int) {
	if t.Signal != nil && t.fg != 0 {
		fg := t.fg
		go t.Signal(fg, sig) // never call back into proc with t.mu held
	}
}

// Flush drops input not yet read (TCFLSH TCIFLUSH, and a keyboard mode
// change: BUG-0020).
func (t *TTY) Flush() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.line, t.ready, t.eof = t.line[:0], t.ready[:0], 0
	t.notify()
}

// Pending is FIONREAD: bytes a read would return now.
func (t *TTY) Pending() int { t.mu.Lock(); defer t.mu.Unlock(); return len(t.ready) }
