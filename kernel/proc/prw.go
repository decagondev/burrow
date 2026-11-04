package proc

// prw.go - pread64 and pwrite64 (W18-S03; memfd users want them): read or
// write at an offset without moving the description's own, for any file
// whose file system can (io.ReaderAt, io.WriterAt: tarfs, memfs, FAT,
// /tmp and FAT writes, shared objects). Pipes, sockets and terminals are
// ESPIPE, as Linux says. Docs: POSIX_COVERAGE.md.

import (
	"io"
	"syscall"
)

// pread64(fd, buf, count, offset).
func sysPreadFn(p *Process, a [6]uint64) (int64, bool) {
	f := p.fds.get(a[0])
	switch {
	case f == nil || f.f == nil || f.flags&oAccMode == oWRONLY:
		return ok(-int64(syscall.EBADF))
	case int64(a[3]) < 0:
		return ok(-int64(syscall.EINVAL))
	}
	r, isR := f.f.(io.ReaderAt)
	if !isR {
		return ok(-int64(syscall.ESPIPE))
	}
	b := make([]byte, min(a[2], maxIO))
	n, err := r.ReadAt(b, int64(a[3]))
	if err != nil && err != io.EOF {
		return ok(errno(err))
	}
	if err := p.mem.CopyOut(a[1], b[:n]); err != nil {
		return ok(errno(err))
	}
	return ok(int64(n))
}

// pwrite64(fd, buf, count, offset).
func sysPwriteFn(p *Process, a [6]uint64) (int64, bool) {
	f := p.fds.get(a[0])
	switch {
	case f == nil || f.f == nil || f.flags&oAccMode == oRDONLY:
		return ok(-int64(syscall.EBADF))
	case int64(a[3]) < 0:
		return ok(-int64(syscall.EINVAL))
	}
	w, isW := f.f.(io.WriterAt)
	if !isW {
		return ok(-int64(syscall.ESPIPE))
	}
	b := make([]byte, min(a[2], maxIO))
	if err := p.mem.CopyIn(a[1], b); err != nil {
		return ok(errno(err))
	}
	n, err := w.WriteAt(b, int64(a[3]))
	if err != nil {
		return ok(errno(err))
	}
	return ok(int64(n))
}

func init() {
	table[17] = sysPreadFn
	table[18] = sysPwriteFn
}
