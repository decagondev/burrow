// Package vfs is the kernel's file tree: a mount table that is itself an
// io/fs.FS (E07.F02). A path goes to the file system mounted at its longest
// matching prefix; listing a directory shows the mount points inside it,
// even when the parent file system has no such directory.
//
// Reading uses io/fs, so every file system (and the VFS as a whole) is
// checked with testing/fstest.TestFS. Writing goes through Writable, which
// read-only file systems (tarfs) simply do not implement. Paths here are
// io/fs paths ("etc/motd", "." for the root); Clean converts "/etc/motd".
package vfs

import (
	"fmt"
	"io/fs"
	"path"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"
)

// Writable is a file system that can change (memfs, devfs).
type Writable interface {
	fs.FS
	WriteFile(name string, data []byte, mode fs.FileMode, mtime time.Time) error
	Mkdir(name string, mode fs.FileMode, mtime time.Time) error
	Remove(name string) error
}

// VFS is safe for concurrent use.
type VFS struct {
	mu     sync.RWMutex
	mounts []mount // longest point first
	now    func() time.Time
}

type mount struct {
	point string // "." is the root
	fsys  fs.FS
	kind  string
}

// MountInfo describes one mount, for `mounts`.
type MountInfo struct{ Point, Kind string }

func New() *VFS { return &VFS{now: time.Now} }

// Clean turns a user path ("/etc/motd", "etc//x/", "/") into an io/fs path.
func Clean(p string) string {
	if c := strings.TrimPrefix(path.Clean("/"+p), "/"); c != "" {
		return c
	}
	return "."
}

// Mount attaches fsys at point (an io/fs path). Points are unique.
func (v *VFS) Mount(point string, fsys fs.FS, kind string) error {
	if !fs.ValidPath(point) {
		return &fs.PathError{Op: "mount", Path: point, Err: fs.ErrInvalid}
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	for _, m := range v.mounts {
		if m.point == point {
			return &fs.PathError{Op: "mount", Path: point, Err: fs.ErrExist}
		}
	}
	v.mounts = append(v.mounts, mount{point, fsys, kind})
	sort.SliceStable(v.mounts, func(i, j int) bool { return depth(v.mounts[i].point) > depth(v.mounts[j].point) })
	return nil
}

func depth(p string) int {
	if p == "." {
		return 0
	}
	return strings.Count(p, "/") + 1
}

// Mounts lists the mount table, root first.
func (v *VFS) Mounts() []MountInfo {
	v.mu.RLock()
	defer v.mu.RUnlock()
	out := make([]MountInfo, len(v.mounts))
	for i, m := range v.mounts {
		out[len(out)-1-i] = MountInfo{"/" + strings.TrimPrefix(m.point, "."), m.kind}
	}
	return out
}

// resolve finds the mount for name and the path inside it.
func (v *VFS) resolve(name string) (mount, string, bool) {
	v.mu.RLock()
	defer v.mu.RUnlock()
	for _, m := range v.mounts {
		switch {
		case m.point == ".":
			return m, name, true
		case name == m.point:
			return m, ".", true
		case strings.HasPrefix(name, m.point+"/"):
			return m, name[len(m.point)+1:], true
		}
	}
	return mount{}, "", false
}

// children are the mount points directly inside directory dir.
func (v *VFS) children(dir string) []mount {
	v.mu.RLock()
	defer v.mu.RUnlock()
	var out []mount
	for _, m := range v.mounts {
		if m.point != "." && path.Dir(m.point) == dir {
			out = append(out, m)
		}
	}
	return out
}

// Open implements fs.FS.
func (v *VFS) Open(name string) (fs.File, error) {
	if !fs.ValidPath(name) {
		return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrInvalid}
	}
	m, rel, ok := v.resolve(name)
	if !ok {
		return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrNotExist}
	}
	f, err := m.fsys.Open(rel)
	if err != nil {
		return nil, &fs.PathError{Op: "open", Path: name, Err: unwrap(err)}
	}
	if rel == "." && name != "." {
		f = renamed{f, path.Base(name)} // a mount root is named after its point
	}
	if st, err := f.Stat(); err == nil && st.IsDir() {
		if kids := v.children(name); len(kids) > 0 {
			return newMergedDir(f, kids)
		}
	}
	return f, nil
}

func unwrap(err error) error {
	if pe, ok := err.(*fs.PathError); ok {
		return pe.Err
	}
	return err
}

func (v *VFS) writable(op, name string) (Writable, string, error) {
	if !fs.ValidPath(name) {
		return nil, "", &fs.PathError{Op: op, Path: name, Err: fs.ErrInvalid}
	}
	m, rel, ok := v.resolve(name)
	if !ok {
		return nil, "", &fs.PathError{Op: op, Path: name, Err: fs.ErrNotExist}
	}
	w, ok := m.fsys.(Writable)
	if !ok || rel == "." {
		return nil, "", &fs.PathError{Op: op, Path: name, Err: fs.ErrPermission}
	}
	return w, rel, nil
}

// MakeSocket creates a socket node (a bound Unix socket's name, W14-S07)
// on a file system that can hold one; elsewhere (FAT, as Linux's vfat)
// it is a permission error.
func (v *VFS) MakeSocket(name string) error {
	w, rel, err := v.writable("bind", name)
	if err != nil {
		return err
	}
	m, ok := w.(interface {
		MakeSocket(name string, mtime time.Time) error
	})
	if !ok {
		return &fs.PathError{Op: "bind", Path: name, Err: fs.ErrPermission}
	}
	return wrap("bind", name, m.MakeSocket(rel, v.now()))
}

// WriteFile creates or replaces a file.
func (v *VFS) WriteFile(name string, data []byte) error {
	w, rel, err := v.writable("write", name)
	if err != nil {
		return err
	}
	return wrap("write", name, w.WriteFile(rel, data, 0o644, v.now()))
}

func (v *VFS) Mkdir(name string) error {
	w, rel, err := v.writable("mkdir", name)
	if err != nil {
		return err
	}
	return wrap("mkdir", name, w.Mkdir(rel, 0o755, v.now()))
}

// Remove deletes a file or an empty directory; a mount point is busy.
func (v *VFS) Remove(name string) error {
	for _, m := range v.children(path.Dir(name)) {
		if m.point == name {
			return &fs.PathError{Op: "remove", Path: name, Err: fs.ErrPermission}
		}
	}
	w, rel, err := v.writable("remove", name)
	if err != nil {
		return err
	}
	return wrap("remove", name, w.Remove(rel))
}

// Renamer is a writable file system that can rename (memfs, fat).
type Renamer interface {
	Rename(oldname, newname string) error
}

// Rename moves oldname to newname inside one mount (EXDEV across mounts,
// as Linux); a mount point is busy.
func (v *VFS) Rename(oldname, newname string) error {
	for _, name := range []string{oldname, newname} {
		for _, m := range v.children(path.Dir(name)) {
			if m.point == name {
				return &fs.PathError{Op: "rename", Path: name, Err: fs.ErrPermission}
			}
		}
	}
	w, from, err := v.writable("rename", oldname)
	if err != nil {
		return err
	}
	w2, to, err := v.writable("rename", newname)
	if err != nil {
		return err
	}
	if w != w2 {
		return &fs.PathError{Op: "rename", Path: newname, Err: syscall.EXDEV}
	}
	r, ok := w.(Renamer)
	if !ok {
		return &fs.PathError{Op: "rename", Path: oldname, Err: fs.ErrPermission}
	}
	return wrap("rename", oldname, r.Rename(from, to))
}

// wrap reports errors with the VFS path, not the path inside the mount.
func wrap(op, name string, err error) error {
	if err == nil {
		return nil
	}
	return &fs.PathError{Op: op, Path: name, Err: unwrap(err)}
}

func (m MountInfo) String() string { return fmt.Sprintf("%-8s on %s", m.Kind, m.Point) }
