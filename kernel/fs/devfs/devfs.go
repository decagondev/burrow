// Package devfs is the /dev file system (E07.F03): device files that drivers
// register by name ("null", "fb0", "input/event0"). Reading opens the
// device through io/fs; vfs.WriteFile on a device writes to it. Devices are
// streams with no stored contents, so a device file reports size 0.
//
// fstest.TestFS cannot check this file system: it reads every file to the
// end, and /dev/zero has no end.
package devfs

import (
	"io"
	"io/fs"
	"path"
	"sort"
	"strings"
	"sync"
	"time"
)

// Device is one device. Read and Write may be called from many goroutines.
// A device may offer more (ioctls, mmap, readiness) through interfaces the
// kernel's file layer looks for on Device() (kernel/proc).
type Device interface {
	io.Reader
	io.Writer
}

// Opener is a device that wants to know about each open: Open returns the
// handle that one open file uses (it may be d itself), and closing the file
// closes the handle if it is an io.Closer.
type Opener interface {
	Open() (Device, error)
}

// FS is safe for concurrent use; drivers may register while files are open.
type FS struct {
	mu   sync.RWMutex
	devs map[string]entry
}

type entry struct {
	dev  Device
	mode fs.FileMode
}

func New() *FS { return &FS{devs: map[string]entry{}} }

// Register adds a device file; name may contain directories ("input/event0").
func (f *FS) Register(name string, d Device, perm fs.FileMode) error {
	if !fs.ValidPath(name) || name == "." {
		return &fs.PathError{Op: "register", Path: name, Err: fs.ErrInvalid}
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, dup := f.devs[name]; dup || f.isDir(name) {
		return &fs.PathError{Op: "register", Path: name, Err: fs.ErrExist}
	}
	f.devs[name] = entry{d, fs.ModeDevice | fs.ModeCharDevice | perm.Perm()}
	return nil
}

// isDir: some device lives below name. Caller holds mu.
func (f *FS) isDir(name string) bool {
	if name == "." {
		return true
	}
	for n := range f.devs {
		if strings.HasPrefix(n, name+"/") {
			return true
		}
	}
	return false
}

func (f *FS) Open(name string) (fs.File, error) {
	if !fs.ValidPath(name) {
		return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrInvalid}
	}
	f.mu.RLock()
	defer f.mu.RUnlock()
	if e, ok := f.devs[name]; ok {
		if e.dev == nil { // a socket node (sockets.go): reached with connect, not open
			return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrInvalid}
		}
		d := e.dev
		if o, isO := d.(Opener); isO {
			h, err := o.Open()
			if err != nil {
				return nil, &fs.PathError{Op: "open", Path: name, Err: err}
			}
			d = h
		}
		return &devFile{info: info{path.Base(name), e.mode}, dev: d}, nil
	}
	if !f.isDir(name) {
		return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrNotExist}
	}
	return &dirFile{info: info{path.Base(name), fs.ModeDir | 0o755}, entries: f.list(name)}, nil
}

// list is directory name's entries in name order. Caller holds mu.
func (f *FS) list(dir string) []fs.DirEntry {
	prefix := dir + "/"
	if dir == "." {
		prefix = ""
	}
	seen := map[string]fs.DirEntry{}
	for n, e := range f.devs {
		rest, ok := strings.CutPrefix(n, prefix)
		if !ok {
			continue
		}
		if child, _, sub := strings.Cut(rest, "/"); sub {
			seen[child] = fs.FileInfoToDirEntry(info{child, fs.ModeDir | 0o755})
		} else {
			seen[rest] = fs.FileInfoToDirEntry(info{rest, e.mode})
		}
	}
	out := make([]fs.DirEntry, 0, len(seen))
	for _, e := range seen {
		out = append(out, e)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name() < out[j].Name() })
	return out
}

// WriteFile writes data to the device (vfs.Writable).
func (f *FS) WriteFile(name string, data []byte, _ fs.FileMode, _ time.Time) error {
	f.mu.RLock()
	e, ok := f.devs[name]
	f.mu.RUnlock()
	if !ok {
		return &fs.PathError{Op: "write", Path: name, Err: fs.ErrPermission} // no new files in /dev
	}
	_, err := e.dev.Write(data)
	return err
}

func (f *FS) Mkdir(name string, _ fs.FileMode, _ time.Time) error {
	return &fs.PathError{Op: "mkdir", Path: name, Err: fs.ErrPermission}
}

// Remove takes away a socket node (W23-S06); devices stay.
func (f *FS) Remove(name string) error { return f.removeSocket(name) }

type info struct {
	name string
	mode fs.FileMode
}

func (i info) Name() string       { return i.name }
func (i info) Size() int64        { return 0 }
func (i info) Mode() fs.FileMode  { return i.mode }
func (i info) ModTime() time.Time { return time.Time{} }
func (i info) IsDir() bool        { return i.mode.IsDir() }
func (i info) Sys() any           { return nil }

type devFile struct {
	info info
	dev  Device
}

func (d *devFile) Stat() (fs.FileInfo, error)  { return d.info, nil }
func (d *devFile) Read(p []byte) (int, error)  { return d.dev.Read(p) }
func (d *devFile) Write(p []byte) (int, error) { return d.dev.Write(p) }
func (d *devFile) Device() Device              { return d.dev }

func (d *devFile) Close() error {
	if c, ok := d.dev.(io.Closer); ok {
		return c.Close()
	}
	return nil
}

type dirFile struct {
	info    info
	entries []fs.DirEntry
	next    int
}

func (d *dirFile) Stat() (fs.FileInfo, error) { return d.info, nil }
func (d *dirFile) Read([]byte) (int, error) {
	return 0, &fs.PathError{Op: "read", Path: d.info.name, Err: fs.ErrInvalid}
}
func (d *dirFile) Close() error { return nil }

func (d *dirFile) ReadDir(n int) ([]fs.DirEntry, error) {
	left := d.entries[d.next:]
	if n <= 0 {
		d.next = len(d.entries)
		return left, nil
	}
	if len(left) == 0 {
		return nil, io.EOF
	}
	n = min(n, len(left))
	d.next += n
	return left[:n], nil
}

// ---- the standard devices ----

// Null reads as empty and swallows writes.
type Null struct{}

func (Null) Read([]byte) (int, error)    { return 0, io.EOF }
func (Null) Write(p []byte) (int, error) { return len(p), nil }

// Zero reads as an endless run of zero bytes and swallows writes.
type Zero struct{}

func (Zero) Read(p []byte) (int, error) {
	clear(p)
	return len(p), nil
}
func (Zero) Write(p []byte) (int, error) { return len(p), nil }

// WriteOnly turns a writer (the console) into a device that reads as empty.
type WriteOnly struct{ io.Writer }

func (WriteOnly) Read([]byte) (int, error) { return 0, io.EOF }
