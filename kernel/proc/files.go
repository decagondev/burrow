package proc

import (
	"bytes"
	"errors"
	"io"
	"io/fs"
	"sync"
	"syscall"
)

func bytesReaderAt(b []byte) io.ReaderAt { return bytes.NewReader(b) }

// file is one open file description: a VFS file or the console.
type file struct {
	f       fs.File   // nil for the console
	console io.Writer // non-nil for fds 0-2
}

// fdTable is a process's file descriptors.
type fdTable struct {
	mu  sync.Mutex
	fds map[int]*file
}

const maxFDs = 256

func newFDTable(console io.Writer) *fdTable {
	c := &file{console: console}
	return &fdTable{fds: map[int]*file{0: c, 1: c, 2: c}}
}

func (t *fdTable) get(fd uint64) *file {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.fds[int(int32(fd))]
}

// add takes the lowest free descriptor, as POSIX requires.
func (t *fdTable) add(f *file) (int, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	for fd := 0; fd < maxFDs; fd++ {
		if _, used := t.fds[fd]; !used {
			t.fds[fd] = f
			return fd, nil
		}
	}
	return 0, syscall.EMFILE
}

func (t *fdTable) close(fd uint64) error {
	t.mu.Lock()
	f, ok := t.fds[int(int32(fd))]
	delete(t.fds, int(int32(fd)))
	t.mu.Unlock()
	if !ok {
		return syscall.EBADF
	}
	if f.f != nil && !t.shared(f) {
		return f.f.Close()
	}
	return nil
}

// shared: another descriptor still uses this description (dup comes in W10).
func (t *fdTable) shared(f *file) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	for _, g := range t.fds {
		if g == f {
			return true
		}
	}
	return false
}

func (t *fdTable) closeAll() {
	t.mu.Lock()
	fds := t.fds
	t.fds = map[int]*file{}
	t.mu.Unlock()
	seen := map[*file]bool{}
	for _, f := range fds {
		if f.f != nil && !seen[f] {
			seen[f] = true
			f.f.Close()
		}
	}
}

// errno turns a Go error into the negative Linux errno a syscall returns.
func errno(err error) int64 {
	var e syscall.Errno
	switch {
	case err == nil:
		return 0
	case errors.As(err, &e):
		return -int64(e)
	case errors.Is(err, ErrFault):
		return -int64(syscall.EFAULT)
	case errors.Is(err, fs.ErrNotExist):
		return -int64(syscall.ENOENT)
	case errors.Is(err, fs.ErrPermission):
		return -int64(syscall.EACCES)
	case errors.Is(err, fs.ErrExist):
		return -int64(syscall.EEXIST)
	case errors.Is(err, fs.ErrInvalid):
		return -int64(syscall.EINVAL)
	}
	return -int64(syscall.EIO)
}
