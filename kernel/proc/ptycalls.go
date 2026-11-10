package proc

// ptycalls.go - pseudo-terminals as files (W16-S03: st runs busybox sh on
// one): /dev/ptmx opens a new pair's master (kernel/tty.NewPair), and
// /dev/pts/N its slave, a terminal like the console with its own line
// discipline, foreground group and size. musl's openpty asks TIOCSPTLCK
// and TIOCGPTN on the master. A process has a controlling terminal
// (TIOCSCTTY on a slave makes it so, for its whole session's children):
// /dev/tty opens it, else the console. Never blocks a writer on echo; the
// pair's rules are in kernel/tty/pty.go. Docs: POSIX_COVERAGE.md.

import (
	"encoding/binary"
	"io/fs"
	"strconv"
	"strings"
	"syscall"

	"burrow/kernel/tty"
)

const (
	tiocgptn   = 0x80045430
	tiocsptlck = 0x40045431
	ptyRows    = 24 // a new pair's size, until the emulator sets its own
	ptyCols    = 80
	ptsPrefix  = "dev/pts/"
)

// ptmFile is an open master.
type ptmFile struct {
	m   *tty.Master
	n   int
	sys *System
}

func (f ptmFile) Read(b []byte) (int, error)  { return f.m.Read(b) }
func (f ptmFile) Write(b []byte) (int, error) { return f.m.Write(b) }
func (f ptmFile) ReadCancel(b []byte, c <-chan struct{}) (int, error) {
	return f.m.ReadCancel(b, c)
}
func (f ptmFile) WriteCancel(b []byte, c <-chan struct{}) (int, error) {
	return f.m.WriteCancel(b, c)
}
func (f ptmFile) Poll() (int16, <-chan struct{}) { return f.m.Poll() }
func (f ptmFile) Stat() (fs.FileInfo, error)     { return ttyInfo{}, nil }

func (f ptmFile) Close() error {
	f.sys.mu.Lock()
	delete(f.sys.ptys, f.n)
	delete(f.sys.ptyAttrs, f.n)
	f.sys.mu.Unlock()
	return f.m.Close()
}

// openPTY is /dev/ptmx (a new pair, the lowest free number) or /dev/pts/N.
func (s *System) openPTY(name string, uid, gid uint32) (*file, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if name == "dev/ptmx" {
		if s.ptys == nil {
			s.ptys = map[int]*tty.Master{}
		}
		n := 0
		for s.ptys[n] != nil {
			n++
		}
		m := tty.NewPair(ptyRows, ptyCols)
		m.Slave().Signal = func(pgrp, sig int) { s.KillGroup(pgrp, sig) }
		s.ptys[n] = m
		if s.ptyAttrs == nil {
			s.ptyAttrs = map[int]*ptyAttr{}
		}
		s.ptyAttrs[n] = &ptyAttr{uid, gid, ptsMode} // the opener's (ptyowner.go)
		return newFile(ptmFile{m, n, s}, nil), nil
	}
	n, err := strconv.Atoi(strings.TrimPrefix(name, ptsPrefix))
	m := s.ptys[n]
	if err != nil || m == nil {
		return nil, syscall.ENOENT
	}
	m.OpenSlave()
	return newFile(ttyFile{m.Slave(), m}, nil), nil
}

func isPTYPath(name string) bool { return name == "dev/ptmx" || strings.HasPrefix(name, ptsPrefix) }

// terminal is the TTY an ioctl on f speaks to: the console, a slave, or
// the slave behind a master (termios and the size are the pair's).
func terminal(f *file) (t *tty.TTY, master *ptmFile) {
	switch x := f.f.(type) {
	case ttyFile:
		return x.t, nil
	case ptmFile:
		return x.m.Slave(), &x
	}
	return nil, nil
}

// ptyIoctl answers the master's own requests and the controlling-terminal
// ones; handled is false for the rest, which every terminal shares.
func (p *Process) ptyIoctl(t *tty.TTY, master *ptmFile, req, arg uint64) (int64, bool) {
	putInt := func(n int) int64 {
		var b [4]byte
		binary.LittleEndian.PutUint32(b[:], uint32(n))
		return errno(p.mem.CopyOut(arg, b[:]))
	}
	switch {
	case master != nil && req == tiocgptn:
		return putInt(master.n), true
	case master != nil && req == tiocsptlck:
		return 0, true // nothing to lock: a slave opens when its master exists
	case master != nil && req == fionread:
		return putInt(master.m.Pending()), true
	case req == tiocsctty:
		p.mu.Lock()
		p.ctty = t
		p.mu.Unlock()
		t.SetForeground(p.PGID)
		return 0, true
	case req == tiocnotty:
		p.mu.Lock()
		p.ctty = nil
		p.mu.Unlock()
		return 0, true
	}
	return 0, false
}

// controlling is /dev/tty for p: its controlling terminal, else the console.
func (p *Process) controlling() *tty.TTY {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.ctty
}
