package tty

// pty.go - pseudo-terminal pairs (W16-S03: st runs a shell on one). The
// slave is an ordinary TTY, line discipline and all; the master is the
// terminal emulator's end: what it writes is the slave's typing, and what
// it reads is the slave's output, processed as Linux's n_tty does (OPOST
// and ONLCR, so a program's NL reaches st as CR NL). Echo never waits for
// the master (a single-threaded emulator writing input would deadlock);
// program output waits while the master holds MasterBuffer bytes. When the
// last slave file closes, master reads drain and then fail with EIO; when
// the master closes, the slave hangs up (end of file, EIO on write, SIGHUP
// to its foreground group). Docs: docs/02-architecture/POSIX_COVERAGE.md.

import (
	"sync"
	"syscall"
)

// MasterBuffer is how much slave output the master holds unread.
const MasterBuffer = 64 << 10

// poll(2) bits, for files built on a pair.
const (
	PollIn  = 0x1
	PollOut = 0x4
	PollHup = 0x10
)

// Master is the master side of a pair.
type Master struct {
	mu      sync.Mutex
	buf     []byte // slave output not yet read
	changed chan struct{}
	slave   *TTY
	slaves  int  // open slave files
	opened  bool // a slave was opened: from then, none open is a hang-up
	closed  bool
}

// NewPair makes a pair with the slave's window size.
func NewPair(rows, cols int) *Master {
	m := &Master{changed: make(chan struct{})}
	m.slave = New(masterSink{m}, rows, cols)
	m.slave.opost = true
	return m
}

func (m *Master) Slave() *TTY { return m.slave }

func (m *Master) notify() { close(m.changed); m.changed = make(chan struct{}) }

// OpenSlave and CloseSlave count the slave's open files.
func (m *Master) OpenSlave() { m.mu.Lock(); m.slaves++; m.opened = true; m.mu.Unlock() }

func (m *Master) CloseSlave() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.slaves--
	m.notify()
}

func (m *Master) slavesGone() bool { return m.opened && m.slaves == 0 }

func (m *Master) Read(p []byte) (int, error) { return m.ReadCancel(p, nil) }

// ReadCancel waits for slave output; EIO once every slave file is closed
// and nothing is left.
func (m *Master) ReadCancel(p []byte, cancel <-chan struct{}) (int, error) {
	for {
		m.mu.Lock()
		if len(m.buf) > 0 {
			n := copy(p, m.buf)
			m.buf = m.buf[n:]
			m.notify() // room for a waiting writer
			m.mu.Unlock()
			return n, nil
		}
		if m.slavesGone() {
			m.mu.Unlock()
			return 0, syscall.EIO
		}
		ch := m.changed
		m.mu.Unlock()
		select {
		case <-ch:
		case <-cancel:
			return 0, syscall.EINTR
		}
	}
}

// Write is typing on the slave's terminal.
func (m *Master) Write(p []byte) (int, error) {
	for _, b := range p {
		m.slave.Input(b)
	}
	return len(p), nil
}

func (m *Master) WriteCancel(p []byte, _ <-chan struct{}) (int, error) { return m.Write(p) }

// Close hangs up the slave.
func (m *Master) Close() error {
	m.mu.Lock()
	m.closed = true
	m.notify()
	m.mu.Unlock()
	m.slave.Hangup()
	return nil
}

// Poll: readable with output waiting or after the slaves closed; input is
// always taken.
func (m *Master) Poll() (int16, <-chan struct{}) {
	m.mu.Lock()
	defer m.mu.Unlock()
	rev := int16(PollOut)
	if len(m.buf) > 0 {
		rev |= PollIn
	}
	if m.slavesGone() {
		rev |= PollIn | PollHup
	}
	return rev, m.changed
}

// Pending is FIONREAD on the master.
func (m *Master) Pending() int { m.mu.Lock(); defer m.mu.Unlock(); return len(m.buf) }

// masterSink is the slave's output: Write is echo (never waits, drops what
// does not fit), WriteCancel a program's write (waits for room).
type masterSink struct{ m *Master }

func (k masterSink) Write(p []byte) (int, error) {
	m := k.m
	m.mu.Lock()
	defer m.mu.Unlock()
	room := MasterBuffer - len(m.buf)
	m.buf = append(m.buf, p[:min(len(p), max(room, 0))]...)
	m.notify()
	return len(p), nil
}

func (k masterSink) WriteCancel(p []byte, cancel <-chan struct{}) (int, error) {
	m, n := k.m, 0
	for {
		m.mu.Lock()
		if m.closed {
			m.mu.Unlock()
			return n, syscall.EIO
		}
		if take := min(MasterBuffer-len(m.buf), len(p)); take > 0 {
			m.buf = append(m.buf, p[:take]...)
			p, n = p[take:], n+take
			m.notify()
		}
		if len(p) == 0 {
			m.mu.Unlock()
			return n, nil
		}
		ch := m.changed
		m.mu.Unlock()
		select {
		case <-ch:
		case <-cancel:
			return n, syscall.EINTR
		}
	}
}

// cook applies output processing to p (t.mu held): NL becomes CR NL on a
// pty slave with OPOST and ONLCR. The console draws NL as a new line itself.
func (t *TTY) cook(p []byte) []byte {
	if !t.opost || t.termios.Oflag&(OPOST|ONLCR) != OPOST|ONLCR {
		return p
	}
	out := make([]byte, 0, len(p)+8)
	for _, b := range p {
		if b == '\n' {
			out = append(out, '\r')
		}
		out = append(out, b)
	}
	return out
}

// Hangup is the master closing: reads see end of file, writes fail, and
// the foreground group gets SIGHUP.
func (t *TTY) Hangup() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.hung = true
	t.notify()
	t.signalFG(SIGHUP)
}
