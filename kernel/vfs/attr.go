package vfs

// attr.go - file attributes through the VFS (W19-S01, S04): Attr and NewFile
// are kernel/fsattr's (which file systems import; vfs's own tests import
// memfs, so the types live below both), and the owner-aware calls: create
// with an owner, chmod and chown. A file system without fsattr.Owner (FAT,
// tarfs, devfs) creates files as root and refuses chmod and chown with
// EPERM, as Linux's vfat does. Never decides who may: kernel/proc does.
// Docs: docs/04-plan/waves/WAVE-19-unix.md.

import (
	"io/fs"
	"syscall"

	"burrow/kernel/fsattr"
)

// Attr is fsattr.Attr.
type Attr = fsattr.Attr

// NewFile is fsattr.New: what a file is created with.
type NewFile = fsattr.New

// AttrOf is info's Attr, and whether the file system gave one.
func AttrOf(info fs.FileInfo) (Attr, bool) { return fsattr.Of(info) }

// WriteFileAs creates name with n's owner and mode, or replaces its data.
func (v *VFS) WriteFileAs(name string, data []byte, n NewFile) error {
	w, rel, err := v.writable("write", name)
	if err != nil {
		return err
	}
	if o, ok := w.(fsattr.Owner); ok {
		return wrap("write", name, o.WriteFileAs(rel, data, n))
	}
	return wrap("write", name, w.WriteFile(rel, data, n.Mode, n.Mtime))
}

// MkdirAs makes a directory with n's owner and mode.
func (v *VFS) MkdirAs(name string, n NewFile) error {
	w, rel, err := v.writable("mkdir", name)
	if err != nil {
		return err
	}
	if o, ok := w.(fsattr.Owner); ok {
		return wrap("mkdir", name, o.MkdirAs(rel, n))
	}
	return wrap("mkdir", name, w.Mkdir(rel, n.Mode, n.Mtime))
}

// MakeSocketAs makes a socket node with n's owner and mode.
func (v *VFS) MakeSocketAs(name string, n NewFile) error {
	w, rel, err := v.writable("bind", name)
	if err != nil {
		return err
	}
	if o, ok := w.(fsattr.Owner); ok {
		return wrap("bind", name, o.MakeSocketAs(rel, n))
	}
	return v.MakeSocket(name)
}

// owner is the fsattr.Owner holding name, and name inside it ("." for a
// mount's root, which chmod may change: /tmp's mode is its root's).
func (v *VFS) owner(op, name string) (fsattr.Owner, string, error) {
	if !fs.ValidPath(name) {
		return nil, "", &fs.PathError{Op: op, Path: name, Err: fs.ErrInvalid}
	}
	m, rel, ok := v.resolve(name)
	if !ok {
		return nil, "", &fs.PathError{Op: op, Path: name, Err: fs.ErrNotExist}
	}
	o, isO := m.fsys.(fsattr.Owner)
	if !isO {
		return nil, "", &fs.PathError{Op: op, Path: name, Err: syscall.EPERM}
	}
	return o, rel, nil
}

// Chmod sets name's permission, set-id and sticky bits.
func (v *VFS) Chmod(name string, mode fs.FileMode) error {
	o, rel, err := v.owner("chmod", name)
	if err != nil {
		return err
	}
	return wrap("chmod", name, o.Chmod(rel, mode))
}

// Chown sets name's owner and group (fsattr.Keep leaves one).
func (v *VFS) Chown(name string, uid, gid uint32) error {
	o, rel, err := v.owner("chown", name)
	if err != nil {
		return err
	}
	return wrap("chown", name, o.Chown(rel, uid, gid))
}
