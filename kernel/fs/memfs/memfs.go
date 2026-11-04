// Package memfs is an in-memory file tree that implements io/fs. It is the
// storage behind two file systems: tarfs fills one from the initrd and
// freezes it (read-only, file data are slices of the archive: no copy), and
// ramfs is a writable one for /tmp. Directories list in name order, which
// testing/fstest's conformance checks rely on. Since W19-S04 nodes have an
// owner, a group and an inode number (owners.go: fsattr.Owner).
package memfs

import (
	"bytes"
	"io"
	"io/fs"
	"path"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"

	"burrow/kernel/fsattr"
)

// FS is safe for concurrent use. Open files see the contents as they were
// at Open (a later WriteFile replaces data, it never edits it in place).
type FS struct {
	mu       sync.RWMutex
	root     *node
	readOnly bool
	lastIno  uint64
}

type node struct {
	name     string
	mode     fs.FileMode
	modTime  time.Time
	data     []byte
	children map[string]*node // directories only
	uid, gid uint32
	ino      uint64
	ctime    time.Time
}

// New returns an empty, writable tree.
func New() *FS {
	return &FS{root: &node{name: ".", mode: fs.ModeDir | 0o755, children: map[string]*node{}, ino: 1}, lastIno: 1}
}

// Freeze makes the tree read-only: every later write fails with fs.ErrPermission.
func (f *FS) Freeze() { f.mu.Lock(); f.readOnly = true; f.mu.Unlock() }

func (f *FS) lookup(name string) (*node, bool) {
	n := f.root
	if name == "." {
		return n, true
	}
	for _, part := range strings.Split(name, "/") {
		c, ok := n.children[part]
		if !ok {
			return nil, false
		}
		n = c
	}
	return n, true
}

// Open implements fs.FS.
func (f *FS) Open(name string) (fs.File, error) {
	if !fs.ValidPath(name) {
		return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrInvalid}
	}
	f.mu.RLock()
	defer f.mu.RUnlock()
	n, ok := f.lookup(name)
	if !ok {
		return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrNotExist}
	}
	info := n.info()
	if n.mode.IsDir() {
		return &dir{info: info, entries: n.entries()}, nil
	}
	return &file{info: info, r: bytes.NewReader(n.data)}, nil
}

// parent returns the directory that will hold name, checking it is writable.
func (f *FS) parent(op, name string) (*node, string, error) {
	if !fs.ValidPath(name) || name == "." {
		return nil, "", &fs.PathError{Op: op, Path: name, Err: fs.ErrInvalid}
	}
	if f.readOnly {
		return nil, "", &fs.PathError{Op: op, Path: name, Err: fs.ErrPermission}
	}
	p, ok := f.lookup(path.Dir(name))
	if !ok || !p.mode.IsDir() {
		return nil, "", &fs.PathError{Op: op, Path: name, Err: fs.ErrNotExist}
	}
	return p, path.Base(name), nil
}

// WriteFile creates or replaces a regular file as root (WriteFileAs);
// its directory must exist. The tree keeps data: the caller must not
// change it afterwards.
func (f *FS) WriteFile(name string, data []byte, mode fs.FileMode, mtime time.Time) error {
	return f.WriteFileAs(name, data, fsattr.New{Mode: mode, Mtime: mtime})
}

// MakeSocket creates a socket node (W14-S07) as root, mode 0755: it holds
// no data; the kernel's table of bound names says which socket it is.
func (f *FS) MakeSocket(name string, mtime time.Time) error {
	return f.MakeSocketAs(name, fsattr.New{Mode: 0o755, Mtime: mtime})
}

// Mkdir creates one directory as root; MkdirAll creates every missing one
// on the way.
func (f *FS) Mkdir(name string, mode fs.FileMode, mtime time.Time) error {
	return f.MkdirAs(name, fsattr.New{Mode: mode, Mtime: mtime})
}

func (f *FS) MkdirAll(name string, mode fs.FileMode, mtime time.Time) error {
	if name == "." {
		return nil
	}
	if err := f.MkdirAll(path.Dir(name), mode, mtime); err != nil {
		return err
	}
	f.mu.RLock()
	n, ok := f.lookup(name)
	f.mu.RUnlock()
	switch {
	case ok && n.mode.IsDir():
		return nil
	case ok:
		return &fs.PathError{Op: "mkdir", Path: name, Err: fs.ErrExist}
	}
	return f.Mkdir(name, mode, mtime)
}

// Remove deletes a file or an empty directory.
func (f *FS) Remove(name string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	p, base, err := f.parent("remove", name)
	if err != nil {
		return err
	}
	n, ok := p.children[base]
	switch {
	case !ok:
		return &fs.PathError{Op: "remove", Path: name, Err: fs.ErrNotExist}
	case n.mode.IsDir() && len(n.children) > 0:
		return &fs.PathError{Op: "remove", Path: name, Err: fs.ErrExist}
	}
	delete(p.children, base)
	return nil
}

// Rename moves oldname to newname as rename(2) does: a file replaces a
// file, a directory replaces an empty directory, and a directory never
// moves into itself.
func (f *FS) Rename(oldname, newname string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	po, bo, err := f.parent("rename", oldname)
	if err != nil {
		return err
	}
	pn, bn, err := f.parent("rename", newname)
	if err != nil {
		return err
	}
	n, ok := po.children[bo]
	if !ok {
		return &fs.PathError{Op: "rename", Path: oldname, Err: fs.ErrNotExist}
	}
	if oldname == newname {
		return nil
	}
	if err := renameCheck(n.mode.IsDir(), oldname, newname, pn.children[bn]); err != nil {
		return &fs.PathError{Op: "rename", Path: newname, Err: err}
	}
	delete(po.children, bo)
	n.name = bn
	pn.children[bn] = n
	return nil
}

// renameCheck is rename(2)'s rule for moving oldname (a directory or not)
// onto newname, where dst is whatever is there now (nil for nothing).
func renameCheck(dir bool, oldname, newname string, dst *node) error {
	switch {
	case dir && strings.HasPrefix(newname, oldname+"/"):
		return syscall.EINVAL
	case dst == nil:
		return nil
	case dir && !dst.mode.IsDir():
		return syscall.ENOTDIR
	case !dir && dst.mode.IsDir():
		return syscall.EISDIR
	case dst.mode.IsDir() && len(dst.children) > 0:
		return syscall.ENOTEMPTY
	}
	return nil
}

// Counts reports regular files, directories (root included) and file bytes.
func (f *FS) Counts() (files, dirs int, size int64) {
	f.mu.RLock()
	defer f.mu.RUnlock()
	var walk func(n *node)
	walk = func(n *node) {
		if !n.mode.IsDir() {
			files++
			size += int64(len(n.data))
			return
		}
		dirs++
		for _, c := range n.children {
			walk(c)
		}
	}
	walk(f.root)
	return
}

// ---- open files ----

type info struct {
	name    string
	size    int64
	mode    fs.FileMode
	modTime time.Time
	attr    fsattr.Attr
}

func (n *node) info() *info {
	return &info{name: n.name, size: int64(len(n.data)), mode: n.mode, modTime: n.modTime, attr: n.attr()}
}

func (i *info) Name() string       { return i.name }
func (i *info) Size() int64        { return i.size }
func (i *info) Mode() fs.FileMode  { return i.mode }
func (i *info) ModTime() time.Time { return i.modTime }
func (i *info) IsDir() bool        { return i.mode.IsDir() }
func (i *info) Sys() any           { return i.attr }

func (n *node) entries() []fs.DirEntry {
	out := make([]fs.DirEntry, 0, len(n.children))
	for _, c := range n.children {
		out = append(out, fs.FileInfoToDirEntry(c.info()))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name() < out[j].Name() })
	return out
}

type file struct {
	info *info
	r    *bytes.Reader
}

func (f *file) Stat() (fs.FileInfo, error) { return f.info, nil }
func (f *file) Read(p []byte) (int, error) { return f.r.Read(p) }
func (f *file) Seek(off int64, whence int) (int64, error) {
	return f.r.Seek(off, whence)
}
func (f *file) ReadAt(p []byte, off int64) (int, error) { return f.r.ReadAt(p, off) }
func (f *file) Close() error                            { return nil }

type dir struct {
	info    *info
	entries []fs.DirEntry
	next    int
}

func (d *dir) Stat() (fs.FileInfo, error) { return d.info, nil }
func (d *dir) Read([]byte) (int, error) {
	return 0, &fs.PathError{Op: "read", Path: d.info.name, Err: fs.ErrInvalid}
}
func (d *dir) Close() error { return nil }

// ReadDir follows fs.ReadDirFile: n > 0 returns at most n and io.EOF at the
// end; n <= 0 returns everything left and a nil error.
func (d *dir) ReadDir(n int) ([]fs.DirEntry, error) {
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
