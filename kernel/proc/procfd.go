package proc

// procfd.go - opening /proc/<pid>/fd/N (W23-S03), Linux's "magic links":
// a descriptor opened at a path reads as that path, and the walker follows
// it, so opening it reopens the file by its path (a new description, with
// the permission checks of any open); a pipe, a socket, an eventfd, the
// console or a memfd has no path, so opening its link gives the caller
// the same description. Only a process that may trace the owner gets
// either. Pseudo-terminals are a table, not files of the tree: stat of
// /dev/pts/N and /dev/ptmx is answered here, so ttyname's check (stat of
// the link's target against fstat of the descriptor) holds.
// Docs: WAVE-23-proc-virtio.md.

import (
	"io/fs"
	"strconv"
	"strings"
	"syscall"
)

// openProcFD opens /proc/<pid>/fd/N when N has no path; done is false for
// every other name (the usual open follows a path's link).
func (p *Process) openProcFD(dirfd, nameVA uint64, flags int) (res int64, done bool) {
	name, err := p.atNoFollow(dirfd, nameVA)
	if err != nil || p.kindOf(name) != "proc" {
		return 0, false
	}
	parts := strings.Split(name, "/")
	if len(parts) != 4 || parts[2] != "fd" {
		return 0, false
	}
	pid, err1 := strconv.Atoi(parts[1])
	n, err2 := strconv.Atoi(parts[3])
	s := p.sys
	s.mu.Lock()
	q := s.byPID[pid]
	s.mu.Unlock()
	if err1 != nil || err2 != nil || q == nil {
		return 0, false
	}
	d := q.fds.get(uint64(n))
	if d == nil || strings.HasPrefix(target(d), "/") && d.console == nil && !strings.HasSuffix(target(d), " (deleted)") {
		return 0, false // a path: the walker follows it
	}
	if !s.MayTrace(p.caller(), pid) {
		return -int64(syscall.EACCES), true
	}
	got, err := p.fds.add(d.ref(), 0, flags&oCloexec != 0)
	if err != nil {
		d.unref()
		return errno(err), true
	}
	return int64(got), true
}

// ptyInfo describes /dev/ptmx and an open pair's /dev/pts/N.
func (s *System) ptyInfo(name string) (fs.FileInfo, error) {
	if name == "dev/ptmx" {
		return ttyInfo{}, nil
	}
	n, err := strconv.Atoi(strings.TrimPrefix(name, ptsPrefix))
	s.mu.Lock()
	defer s.mu.Unlock()
	if err != nil || s.ptys[n] == nil {
		return nil, &fs.PathError{Op: "stat", Path: name, Err: fs.ErrNotExist}
	}
	return ttyInfo{}, nil
}
