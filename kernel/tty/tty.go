// Package tty is the terminal line discipline (E09.F04): what sits between
// the keyboard/serial bytes and a program's read(0). In canonical mode it
// collects a line (echo, erase, kill, ^D), in raw mode it passes bytes on
// at once; ISIG turns ^C, ^\ and ^Z into signals for the foreground
// process group. Linux's termios layout and defaults, so musl's isatty,
// tcgetattr and cfmakeraw work unchanged. Pure: tested on the host.
package tty

import (
	"io"
	"sync"
	"syscall"
)

// termios flags (Linux asm-generic/termbits.h).
const (
	ICRNL  = 0x100 // iflag: CR -> NL on input
	OPOST  = 0x1   // oflag
	ONLCR  = 0x4
	CS8    = 0x30 // cflag
	CREAD  = 0x80
	B38400 = 0xF
	ISIG   = 0x1 // lflag
	ICANON = 0x2
	ECHO   = 0x8
	ECHOE  = 0x10
	ECHOK  = 0x20
	ECHONL = 0x40
	IEXTEN = 0x8000

	VINTR  = 0 // c_cc indices
	VQUIT  = 1
	VERASE = 2
	VKILL  = 3
	VEOF   = 4
	VTIME  = 5
	VMIN   = 6
	VSUSP  = 10
	NCCS   = 19

	SIGINT  = 2
	SIGQUIT = 3
	SIGTSTP = 20
)

// Termios is the kernel's struct termios (36 bytes; not glibc's).
type Termios struct {
	Iflag, Oflag, Cflag, Lflag uint32
	Line                       uint8
	CC                         [NCCS]uint8
}

// Default is a fresh terminal: canonical mode with echo and signals.
func Default() Termios {
	t := Termios{Iflag: ICRNL, Oflag: OPOST | ONLCR, Cflag: CS8 | CREAD | B38400,
		Lflag: ISIG | ICANON | ECHO | ECHOE | ECHOK | IEXTEN}
	t.CC[VINTR], t.CC[VQUIT], t.CC[VERASE], t.CC[VKILL] = 0x03, 0x1C, 0x7F, 0x15
	t.CC[VEOF], t.CC[VTIME], t.CC[VMIN], t.CC[VSUSP] = 0x04, 0, 1, 0x1A
	return t
}

// TTY is one terminal. Input comes from Input (driver goroutines), output
// goes to the writer it was made with; reads block until there is input.
type TTY struct {
	mu      sync.Mutex
	out     io.Writer
	termios Termios
	line    []byte // canonical: the line being edited
	ready   []byte // what read may return now
	eof     int    // pending end-of-file marks (^D on an empty line)
	changed chan struct{}
	fg      int // foreground process group
	readers int // blocked in ReadCancel
	rows    uint16
	cols    uint16

	// Signal sends sig to process group pgrp (set by the kernel).
	Signal func(pgrp, sig int)
}

func New(out io.Writer, rows, cols int) *TTY {
	return &TTY{out: out, termios: Default(), changed: make(chan struct{}), rows: uint16(rows), cols: uint16(cols)}
}

func (t *TTY) notify() { close(t.changed); t.changed = make(chan struct{}) }

func (t *TTY) echo(b ...byte) {
	if t.termios.Lflag&ECHO != 0 {
		t.out.Write(b)
	}
}

// Input is one byte typed on the keyboard or received on the serial line.
func (t *TTY) Input(b byte) {
	t.mu.Lock()
	defer t.mu.Unlock()
	tm := &t.termios
	if b == '\r' && tm.Iflag&ICRNL != 0 {
		b = '\n'
	}
	if tm.Lflag&ISIG != 0 {
		sig := 0
		switch b {
		case tm.CC[VINTR]:
			sig = SIGINT
		case tm.CC[VQUIT]:
			sig = SIGQUIT
		case tm.CC[VSUSP]:
			sig = SIGTSTP
		}
		if sig != 0 {
			t.line, t.ready = t.line[:0], t.ready[:0] // flush, as Linux does without NOFLSH
			t.echo('^', b+'@', '\n')
			if t.Signal != nil && t.fg != 0 {
				fg := t.fg
				go t.Signal(fg, sig) // never call back into proc with t.mu held
			}
			t.notify()
			return
		}
	}
	if tm.Lflag&ICANON == 0 { // raw: every byte is input at once
		t.ready = append(t.ready, b)
		t.echo(b)
		t.notify()
		return
	}
	switch {
	case b == tm.CC[VERASE] || b == '\b':
		if len(t.line) > 0 {
			t.line = t.line[:len(t.line)-1]
			if tm.Lflag&ECHOE != 0 {
				t.echo('\b', ' ', '\b')
			}
		}
	case b == tm.CC[VKILL]:
		for range t.line {
			t.echo('\b', ' ', '\b')
		}
		t.line = t.line[:0]
	case b == tm.CC[VEOF]:
		if len(t.line) == 0 {
			t.eof++
		}
		t.ready = append(t.ready, t.line...)
		t.line = t.line[:0]
		t.notify()
	case b == '\n':
		t.line = append(t.line, '\n')
		t.ready = append(t.ready, t.line...)
		t.line = t.line[:0]
		if tm.Lflag&ECHO != 0 || tm.Lflag&ECHONL != 0 {
			t.out.Write([]byte{'\n'})
		}
		t.notify()
	case b >= 0x20 || b == '\t':
		t.line = append(t.line, b)
		t.echo(b)
	}
}

// ReadCancel blocks until input is ready: a whole line in canonical mode
// (at most len(p) of it), end of file after ^D, or EINTR on cancel.
func (t *TTY) ReadCancel(p []byte, cancel <-chan struct{}) (int, error) {
	for {
		t.mu.Lock()
		if len(t.ready) > 0 {
			n := copy(p, t.ready)
			if t.termios.Lflag&ICANON != 0 { // never past the end of a line
				if i := indexByte(t.ready[:n], '\n'); i >= 0 {
					n = i + 1
				}
			}
			t.ready = t.ready[n:]
			t.mu.Unlock()
			return n, nil
		}
		if t.eof > 0 {
			t.eof--
			t.mu.Unlock()
			return 0, nil
		}
		ch := t.changed
		t.readers++
		t.mu.Unlock()
		select {
		case <-ch:
		case <-cancel:
			t.mu.Lock()
			t.readers--
			t.mu.Unlock()
			return 0, syscall.EINTR
		}
		t.mu.Lock()
		t.readers--
		t.mu.Unlock()
	}
}

// Poll reports whether a read would return at once (input or end of file)
// and a channel closed at the next change (poll(2)).
func (t *TTY) Poll() (readable bool, changed <-chan struct{}) {
	t.mu.Lock()
	defer t.mu.Unlock()
	return len(t.ready) > 0 || t.eof > 0, t.changed
}

// Waiting counts a poller waiting for input as a reader (+1, then -1),
// so HasReader is true while it waits.
func (t *TTY) Waiting(delta int) { t.mu.Lock(); t.readers += delta; t.mu.Unlock() }

// HasReader: a program is waiting for input right now.
func (t *TTY) HasReader() bool { t.mu.Lock(); defer t.mu.Unlock(); return t.readers > 0 }

// IsSignal: b would raise a signal (ISIG on and b is INTR, QUIT or SUSP).
func (t *TTY) IsSignal(b byte) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	tm := &t.termios
	return tm.Lflag&ISIG != 0 && (b == tm.CC[VINTR] || b == tm.CC[VQUIT] || b == tm.CC[VSUSP])
}

func indexByte(b []byte, c byte) int {
	for i, x := range b {
		if x == c {
			return i
		}
	}
	return -1
}

// WriteCancel is output: never blocks (the console is fast enough).
func (t *TTY) WriteCancel(p []byte, _ <-chan struct{}) (int, error) { return t.out.Write(p) }

func (t *TTY) Read(p []byte) (int, error)  { return t.ReadCancel(p, nil) }
func (t *TTY) Write(p []byte) (int, error) { return t.out.Write(p) }

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

func (t *TTY) SetSize(rows, cols uint16) { t.mu.Lock(); t.rows, t.cols = rows, cols; t.mu.Unlock() }

// Pending is FIONREAD: bytes a read would return now.
func (t *TTY) Pending() int { t.mu.Lock(); defer t.mu.Unlock(); return len(t.ready) }
