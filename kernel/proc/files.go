package proc

import (
	"bytes"
	"errors"
	"io"
	"io/fs"
	"path"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
)

func bytesReaderAt(b []byte) io.ReaderAt { return bytes.NewReader(b) }

// file is an open file description: a VFS file or the console. fork and
// dup share it (and its offset); the last close closes it.
type file struct {
	f       fs.File   // nil for the console
	console io.Writer // non-nil for the console
	refs    atomic.Int32
	path    string // io/fs path it was opened at ("" for pipes, the console)
	flags   int    // O_ACCMODE and the status flags F_GETFL reports

	mu      sync.Mutex
	dirents []fs.DirEntry // getdents64: entries not returned yet
	dirRead bool          // ...and whether they were read
}

func newFile(f fs.File, console io.Writer) *file {
	d := &file{f: f, console: console, flags: oRDWR}
	d.refs.Store(1)
	return d
}

func (d *file) ref() *file { d.refs.Add(1); return d }

func (d *file) unref() error {
	if d.refs.Add(-1) == 0 && d.f != nil {
		return d.f.Close()
	}
	return nil
}

// fd is one descriptor: a description plus the per-descriptor flag.
type fd struct {
	file    *file
	cloexec bool
}

// fdTable is a process's file descriptors.
type fdTable struct {
	mu  sync.Mutex
	fds map[int]fd
}

const maxFDs = 256

func newFDTable(console io.Writer) *fdTable {
	c := newFile(nil, console)
	c.refs.Store(3)
	return &fdTable{fds: map[int]fd{0: {file: c}, 1: {file: c}, 2: {file: c}}}
}

func (t *fdTable) get(n uint64) *file {
	t.mu.Lock()
	defer t.mu.Unlock()
	if e, ok := t.fds[int(int32(n))]; ok {
		return e.file
	}
	return nil
}

// add takes the lowest free descriptor >= min, as POSIX requires; the table
// owns the reference passed in.
func (t *fdTable) add(f *file, min int, cloexec bool) (int, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	for n := min; n < maxFDs; n++ {
		if _, used := t.fds[n]; !used {
			t.fds[n] = fd{f, cloexec}
			return n, nil
		}
	}
	return 0, syscall.EMFILE
}

// put installs f at n, closing what was there (dup2).
func (t *fdTable) put(n int, f *file, cloexec bool) error {
	if n < 0 || n >= maxFDs {
		return syscall.EBADF
	}
	t.mu.Lock()
	old, had := t.fds[n]
	t.fds[n] = fd{f, cloexec}
	t.mu.Unlock()
	if had {
		old.file.unref()
	}
	return nil
}

func (t *fdTable) close(n uint64) error {
	t.mu.Lock()
	e, ok := t.fds[int(int32(n))]
	delete(t.fds, int(int32(n)))
	t.mu.Unlock()
	if !ok {
		return syscall.EBADF
	}
	return e.file.unref()
}

func (t *fdTable) closeAll() {
	t.mu.Lock()
	fds := t.fds
	t.fds = map[int]fd{}
	t.mu.Unlock()
	for _, e := range fds {
		e.file.unref()
	}
}

// clone is fork's copy: the same descriptions, one more reference each.
func (t *fdTable) clone() *fdTable {
	t.mu.Lock()
	defer t.mu.Unlock()
	c := &fdTable{fds: make(map[int]fd, len(t.fds))}
	for n, e := range t.fds {
		c.fds[n] = fd{e.file.ref(), e.cloexec}
	}
	return c
}

func (t *fdTable) cloexec(n int) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.fds[n].cloexec
}

func (t *fdTable) setCloexec(n int, on bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if e, ok := t.fds[n]; ok {
		e.cloexec = on
		t.fds[n] = e
	}
}

// closeOnExec drops every descriptor marked FD_CLOEXEC (execve).
func (t *fdTable) closeOnExec() {
	t.mu.Lock()
	var drop []*file
	for n, e := range t.fds {
		if e.cloexec {
			drop = append(drop, e.file)
			delete(t.fds, n)
		}
	}
	t.mu.Unlock()
	for _, f := range drop {
		f.unref()
	}
}

// resolve turns a user path into an io/fs path: relative ones start at cwd.
func (p *Process) resolve(name string) string {
	if !strings.HasPrefix(name, "/") {
		p.mu.Lock()
		name = p.cwd + "/" + name
		p.mu.Unlock()
	}
	return cleanPath(name)
}

func cleanPath(name string) string {
	if c := strings.TrimPrefix(path.Clean("/"+name), "/"); c != "" {
		return c
	}
	return "."
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
