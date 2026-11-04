package proc

import (
	"io"
	"io/fs"
	"sync"
	"syscall"
	"time"
)

// WritableFS is the VFS's write side (kernel/vfs.VFS): whole files only.
type WritableFS interface {
	fs.FS
	WriteFile(name string, data []byte) error
	Mkdir(name string) error
	Remove(name string) error
}

// wfile is a regular file opened for writing. The VFS stores whole files,
// so it keeps the contents here and writes them back after every change
// (fine for the small files programs write to /tmp; a page cache can come
// later). Reads see the same buffer (O_RDWR).
type wfile struct {
	mu     sync.Mutex
	fsys   WritableFS
	name   string
	data   []byte
	off    int64
	append bool
}

func (w *wfile) Read(b []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.off >= int64(len(w.data)) {
		return 0, io.EOF
	}
	n := copy(b, w.data[w.off:])
	w.off += int64(n)
	return n, nil
}

func (w *wfile) Write(b []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.append {
		w.off = int64(len(w.data))
	}
	n, err := w.writeAtLocked(b, w.off)
	w.off += int64(n)
	return n, err
}

// WriteAt is pwrite (W18-S03): at off, the offset unmoved.
func (w *wfile) WriteAt(b []byte, off int64) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if off < 0 {
		return 0, fs.ErrInvalid
	}
	return w.writeAtLocked(b, off)
}

func (w *wfile) writeAtLocked(b []byte, off int64) (int, error) {
	end := off + int64(len(b))
	if end > int64(len(w.data)) {
		w.data = append(w.data, make([]byte, end-int64(len(w.data)))...)
	}
	copy(w.data[off:], b)
	if err := w.fsys.WriteFile(w.name, append([]byte(nil), w.data...)); err != nil {
		return 0, err
	}
	return len(b), nil
}

func (w *wfile) Seek(offset int64, whence int) (int64, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	switch whence {
	case io.SeekCurrent:
		offset += w.off
	case io.SeekEnd:
		offset += int64(len(w.data))
	}
	if offset < 0 {
		return 0, fs.ErrInvalid
	}
	w.off = offset
	return offset, nil
}

// Truncate makes the file n bytes long (cut, or zero-filled) and writes it back.
func (w *wfile) Truncate(n int64) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if n < 0 {
		return fs.ErrInvalid
	}
	if n <= int64(len(w.data)) {
		w.data = w.data[:n]
	} else {
		w.data = append(w.data, make([]byte, n-int64(len(w.data)))...)
	}
	return w.fsys.WriteFile(w.name, append([]byte(nil), w.data...))
}

// ReadAt serves mmap (W14-S05): a mapped page fills from the file as it is
// at the first touch.
func (w *wfile) ReadAt(b []byte, off int64) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if off < 0 {
		return 0, fs.ErrInvalid
	}
	if off >= int64(len(w.data)) {
		return 0, io.EOF
	}
	n := copy(b, w.data[off:])
	if n < len(b) {
		return n, io.EOF
	}
	return n, nil
}

func (w *wfile) Close() error { return nil }

func (w *wfile) Stat() (fs.FileInfo, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return wfileInfo{name: baseName(w.name), size: int64(len(w.data))}, nil
}

type wfileInfo struct {
	name string
	size int64
}

func (i wfileInfo) Name() string       { return i.name }
func (i wfileInfo) Size() int64        { return i.size }
func (i wfileInfo) Mode() fs.FileMode  { return 0o644 }
func (i wfileInfo) ModTime() time.Time { return time.Now() }
func (i wfileInfo) IsDir() bool        { return false }
func (i wfileInfo) Sys() any           { return nil }

func baseName(name string) string {
	for i := len(name) - 1; i >= 0; i-- {
		if name[i] == '/' {
			return name[i+1:]
		}
	}
	return name
}

// ftruncate(fd, length): a regular file open for writing (EINVAL otherwise,
// EBADF if not open for writing), as Linux.
func sysFtruncateFn(p *Process, a [6]uint64) (int64, bool) {
	f := p.fds.get(a[0])
	switch {
	case f == nil || f.flags&oAccMode == oRDONLY:
		return ok(-int64(syscall.EBADF))
	case int64(a[1]) < 0:
		return ok(-int64(syscall.EINVAL))
	}
	t, isT := f.f.(interface{ Truncate(int64) error })
	if !isT {
		return ok(-int64(syscall.EINVAL))
	}
	return ok(errno(t.Truncate(int64(a[1]))))
}

// truncate(path, length): open for writing, truncate, close.
func sysTruncateFn(p *Process, a [6]uint64) (int64, bool) {
	fd := p.openAt(uint64(atFDCWD&0xFFFFFFFF), a[0], oWRONLY)
	if fd < 0 {
		return ok(fd)
	}
	defer p.fds.close(uint64(fd))
	return sysFtruncateFn(p, [6]uint64{uint64(fd), a[1]})
}

func init() {
	table[76] = sysTruncateFn
	table[77] = sysFtruncateFn
}
