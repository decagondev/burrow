package proc

// shm.go - shared memory objects (W18-S03): a set of frames several
// processes see the same bytes through, by read and write on a descriptor
// and by mmap(MAP_SHARED). One kind serves /dev/shm (POSIX shm_open and
// shm_unlink, which are open and unlink there), memfd_create, and
// MAP_SHARED|MAP_ANONYMOUS memory, which fork shares. The object owns its
// frames; a page mapped from it is never copy-on-write and never freed by
// a process (page.shm). The frames go when nothing holds the object: no
// open handle, no /dev/shm name, no mapped page (PIT-52). Frames are
// allocated at first touch and read as zero until written.
// Docs: docs/04-plan/waves/WAVE-18-foundations.md, POSIX_COVERAGE.md.

import (
	"io"
	"io/fs"
	"strings"
	"sync"
	"syscall"
	"time"
)

const shmPrefix = "dev/shm/"

type shmObj struct {
	mu     sync.Mutex
	nu     Nucleus
	name   string
	frames map[int64]uint64 // page index -> frame
	size   int64
	refs   int  // open handles, the name, mapped pages
	linked bool // named in /dev/shm
}

func newShmObj(nu Nucleus, name string, size int64) *shmObj {
	return &shmObj{nu: nu, name: name, frames: map[int64]uint64{}, size: size}
}

func (o *shmObj) ref() {
	o.mu.Lock()
	o.refs++
	o.mu.Unlock()
}

// unref drops a holder; the last one frees the frames.
func (o *shmObj) unref() {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.refs--; o.refs > 0 || o.linked {
		return
	}
	for i, pa := range o.frames {
		o.nu.PageFree(pa)
		delete(o.frames, i)
	}
}

// frame is page i's frame, allocated (zeroed) at first touch.
func (o *shmObj) frame(i int64) (uint64, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if pa, ok := o.frames[i]; ok {
		return pa, nil
	}
	pa, err := o.nu.PageAlloc()
	if err != nil {
		return 0, err
	}
	o.frames[i] = pa
	return pa, nil
}

// rw copies between b and the object's bytes at off: write, or read (an
// absent page reads as zeros). Reads stop at the size; writes extend it.
func (o *shmObj) rw(b []byte, off int64, write bool) (int, error) {
	o.mu.Lock()
	size := o.size
	if write && off+int64(len(b)) > size {
		o.size, size = off+int64(len(b)), off+int64(len(b))
	}
	o.mu.Unlock()
	if !write {
		if off >= size {
			return 0, io.EOF
		}
		b = b[:min(int64(len(b)), size-off)]
	}
	for done := 0; done < len(b); {
		at := off + int64(done)
		i, in := at/PageSize, int(at%PageSize)
		k := min(len(b)-done, PageSize-in)
		o.mu.Lock()
		pa, have := o.frames[i]
		o.mu.Unlock()
		switch {
		case have && write:
			copy(o.nu.Frame(pa)[in:in+k], b[done:done+k])
		case have:
			copy(b[done:done+k], o.nu.Frame(pa)[in:in+k])
		case write:
			pa, err := o.frame(i)
			if err != nil {
				return done, err
			}
			copy(o.nu.Frame(pa)[in:in+k], b[done:done+k])
		default:
			clear(b[done : done+k])
		}
		done += k
	}
	return len(b), nil
}

// truncate sets the size; bytes past it read as zero if it grows again.
func (o *shmObj) truncate(n int64) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if n < o.size {
		for i, pa := range o.frames {
			if lo := i * PageSize; lo+PageSize > n {
				clear(o.nu.Frame(pa)[max(n-lo, 0):])
			}
		}
	}
	o.size = n
}

// shmFile is one open handle (an open file description's file).
type shmFile struct {
	o   *shmObj
	mu  sync.Mutex
	off int64
}

func openShm(o *shmObj) *shmFile { o.ref(); return &shmFile{o: o} }

func (f *shmFile) Read(b []byte) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	n, err := f.o.rw(b, f.off, false)
	f.off += int64(n)
	return n, err
}

func (f *shmFile) Write(b []byte) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	n, err := f.o.rw(b, f.off, true)
	f.off += int64(n)
	return n, err
}

func (f *shmFile) ReadAt(b []byte, off int64) (int, error)  { return f.o.rw(b, off, false) }
func (f *shmFile) WriteAt(b []byte, off int64) (int, error) { return f.o.rw(b, off, true) }

func (f *shmFile) Seek(off int64, whence int) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	switch whence {
	case io.SeekCurrent:
		off += f.off
	case io.SeekEnd:
		f.o.mu.Lock()
		off += f.o.size
		f.o.mu.Unlock()
	}
	if off < 0 {
		return 0, syscall.EINVAL
	}
	f.off = off
	return off, nil
}

func (f *shmFile) Truncate(n int64) error { f.o.truncate(n); return nil }
func (f *shmFile) Close() error           { f.o.unref(); return nil }

func (f *shmFile) Stat() (fs.FileInfo, error) {
	f.o.mu.Lock()
	defer f.o.mu.Unlock()
	return shmInfo{name: f.o.name, size: f.o.size}, nil
}

type shmInfo struct {
	name string
	size int64
}

func (i shmInfo) Name() string     { return i.name }
func (i shmInfo) Size() int64      { return i.size }
func (shmInfo) Mode() fs.FileMode  { return 0o600 }
func (shmInfo) ModTime() time.Time { return time.Time{} }
func (shmInfo) IsDir() bool        { return false }
func (shmInfo) Sys() any           { return nil }

// shmTab is /dev/shm: the named objects of a system.
type shmTab struct {
	mu sync.Mutex
	m  map[string]*shmObj
}

func isShmPath(name string) bool {
	return strings.HasPrefix(name, shmPrefix) && len(name) > len(shmPrefix)
}
