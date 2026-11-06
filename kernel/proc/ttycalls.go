package proc

import (
	"encoding/binary"
	"io/fs"
	"syscall"
	"time"

	"burrow/kernel/tty"
)

// SetTerminal makes t the controlling terminal: new programs get it as
// fds 0-2, and its ^C/^\ signal the foreground process group.
func (s *System) SetTerminal(t *tty.TTY) {
	s.mu.Lock()
	s.tty = t
	s.mu.Unlock()
	t.Signal = func(pgrp, sig int) { s.KillGroup(pgrp, sig) }
}

// ttyFile is a terminal as an open file: the console (/dev/tty, fds 0-2)
// or a pseudo-terminal's slave (m is its pair; W16-S03).
type ttyFile struct {
	t *tty.TTY
	m *tty.Master
}

func (f ttyFile) Read(b []byte) (int, error)  { return f.t.Read(b) }
func (f ttyFile) Write(b []byte) (int, error) { return f.t.Write(b) }
func (f ttyFile) ReadCancel(b []byte, c <-chan struct{}) (int, error) {
	return f.t.ReadCancel(b, c)
}
func (f ttyFile) WriteCancel(b []byte, c <-chan struct{}) (int, error) {
	return f.t.WriteCancel(b, c)
}
func (f ttyFile) Poll() (int16, <-chan struct{}) {
	readable, ch := f.t.Poll()
	if readable {
		return pollIn | pollOut, ch
	}
	return pollOut, ch
}
func (f ttyFile) WaitInput(delta int) { f.t.Waiting(delta) }
func (f ttyFile) Close() error {
	if f.m != nil {
		f.m.CloseSlave()
	}
	return nil
}
func (f ttyFile) Stat() (fs.FileInfo, error) { return ttyInfo{}, nil }

type ttyInfo struct{}

func (ttyInfo) Name() string       { return "tty" }
func (ttyInfo) Size() int64        { return 0 }
func (ttyInfo) Mode() fs.FileMode  { return fs.ModeDevice | fs.ModeCharDevice | 0o620 }
func (ttyInfo) ModTime() time.Time { return time.Time{} }
func (ttyInfo) IsDir() bool        { return false }
func (ttyInfo) Sys() any           { return nil }

// stdio is fds 0-2 for a new program: the terminal, else the console.
func (s *System) stdio() *fdTable {
	s.mu.Lock()
	t := s.tty
	s.mu.Unlock()
	if t == nil {
		return newFDTable(s.console)
	}
	f := newFile(ttyFile{t, nil}, nil)
	f.path, f.flags = "dev/tty1", oRDWR // the console's own name: what ttyname finds (W23-S03)
	f.refs.Store(3)
	return &fdTable{fds: map[int]fd{0: {file: f}, 1: {file: f}, 2: {file: f}}}
}

// ioctl requests (Linux asm-generic/ioctls.h)
const (
	tcgets     = 0x5401
	tcsets     = 0x5402
	tcsetsw    = 0x5403
	tcsetsf    = 0x5404
	tiocsctty  = 0x540E
	tiocgpgrp  = 0x540F
	tiocspgrp  = 0x5410
	tiocgwinsz = 0x5413
	tiocswinsz = 0x5414
	fionread   = 0x541B
	tiocnotty  = 0x5422
	tiocgsid   = 0x5429
	termiosLen = 36
)

// ioctl: the terminal's requests; everything else is not a terminal.
func sysIoctlFn(p *Process, a [6]uint64) (int64, bool) {
	a[1] = uint64(uint32(a[1])) // the request is an unsigned int: musl's int arrives sign-extended (BUG-0019)
	f := p.fds.get(a[0])
	if f == nil {
		return ok(-int64(syscall.EBADF))
	}
	if d, isD := device(f).(Ioctler); isD {
		return ok(p.deviceIoctl(d, a[1], a[2]))
	}
	t, master := terminal(f)
	if t == nil {
		return ok(-int64(syscall.ENOTTY))
	}
	if r, handled := p.ptyIoctl(t, master, a[1], a[2]); handled {
		return ok(r)
	}
	le := binary.LittleEndian
	put := func(b []byte) (int64, bool) { return ok(errno(p.mem.CopyOut(a[2], b))) }
	switch a[1] {
	case tcgets:
		tm := t.Termios()
		b := make([]byte, termiosLen)
		le.PutUint32(b[0:], tm.Iflag)
		le.PutUint32(b[4:], tm.Oflag)
		le.PutUint32(b[8:], tm.Cflag)
		le.PutUint32(b[12:], tm.Lflag)
		b[16] = tm.Line
		copy(b[17:], tm.CC[:])
		return put(b)
	case tcsets, tcsetsw, tcsetsf:
		b := make([]byte, termiosLen)
		if err := p.mem.CopyIn(a[2], b); err != nil {
			return ok(errno(err))
		}
		tm := tty.Termios{Iflag: le.Uint32(b[0:]), Oflag: le.Uint32(b[4:]), Cflag: le.Uint32(b[8:]),
			Lflag: le.Uint32(b[12:]), Line: b[16]}
		copy(tm.CC[:], b[17:])
		t.SetTermios(tm, a[1] == tcsetsf)
		return ok(0)
	case tiocgwinsz:
		rows, cols := t.Size()
		b := make([]byte, 8)
		le.PutUint16(b[0:], rows)
		le.PutUint16(b[2:], cols)
		return put(b)
	case tiocswinsz:
		b := make([]byte, 8)
		if err := p.mem.CopyIn(a[2], b); err != nil {
			return ok(errno(err))
		}
		t.SetSize(le.Uint16(b[0:]), le.Uint16(b[2:]))
		return ok(0)
	case tiocgpgrp:
		b := make([]byte, 4)
		le.PutUint32(b, uint32(t.Foreground()))
		return put(b)
	case tiocspgrp:
		b := make([]byte, 4)
		if err := p.mem.CopyIn(a[2], b); err != nil {
			return ok(errno(err))
		}
		t.SetForeground(int(int32(le.Uint32(b))))
		return ok(0)
	case fionread:
		b := make([]byte, 4)
		le.PutUint32(b, uint32(t.Pending()))
		return put(b)
	case tiocgsid:
		b := make([]byte, 4)
		le.PutUint32(b, uint32(p.SID))
		return put(b)
	}
	if master != nil {
		return ok(-int64(syscall.EINVAL))
	}
	if r, handled := p.vtIoctl(a[1], a[2]); handled {
		return ok(r)
	}
	return ok(-int64(syscall.EINVAL))
}

// ---- process groups and sessions ----

func sysSetpgidFn(p *Process, a [6]uint64) (int64, bool) {
	s := p.sys
	s.mu.Lock()
	defer s.mu.Unlock()
	pid, pgid := int(int32(a[0])), int(int32(a[1]))
	if pid == 0 {
		pid = p.PID
	}
	if pgid == 0 {
		pgid = pid
	}
	t := s.byPID[pid]
	switch {
	case pgid < 0:
		return ok(-int64(syscall.EINVAL))
	case t == nil || t != p && t.PPID != p.PID:
		return ok(-int64(syscall.ESRCH))
	case t.SID != p.SID || t.PID == t.SID:
		return ok(-int64(syscall.EPERM))
	}
	t.PGID = pgid
	return ok(0)
}

func sysGetpgidFn(p *Process, a [6]uint64) (int64, bool) {
	s := p.sys
	s.mu.Lock()
	defer s.mu.Unlock()
	t := p
	if a[0] != 0 {
		if t = s.byPID[int(int32(a[0]))]; t == nil {
			return ok(-int64(syscall.ESRCH))
		}
	}
	return ok(int64(t.PGID))
}

func sysGetpgrpFn(p *Process, _ [6]uint64) (int64, bool) { return sysGetpgidFn(p, [6]uint64{}) }

func sysSetsidFn(p *Process, _ [6]uint64) (int64, bool) {
	s := p.sys
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, t := range s.byPID {
		if t.PGID == p.PID && t != p || p.PGID == p.PID {
			return ok(-int64(syscall.EPERM)) // already a group leader
		}
	}
	p.SID, p.PGID = p.PID, p.PID
	p.ctty = nil // a new session has no controlling terminal until TIOCSCTTY
	return ok(int64(p.PID))
}

func sysGetsidFn(p *Process, a [6]uint64) (int64, bool) {
	s := p.sys
	s.mu.Lock()
	defer s.mu.Unlock()
	t := p
	if a[0] != 0 {
		if t = s.byPID[int(int32(a[0]))]; t == nil {
			return ok(-int64(syscall.ESRCH))
		}
	}
	return ok(int64(t.SID))
}

func init() {
	for nr, h := range map[uint64]handler{
		16: sysIoctlFn, 109: sysSetpgidFn, 111: sysGetpgrpFn, 112: sysSetsidFn,
		121: sysGetpgidFn, 124: sysGetsidFn,
	} {
		table[nr] = h
	}
}
