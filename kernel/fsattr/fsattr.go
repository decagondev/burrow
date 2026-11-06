// Package fsattr is what a file system knows about a file beyond io/fs
// (W19-S01, S04): its owner, links and inode as fs.FileInfo.Sys(), the
// owner and mode a new file is created with, and the interface of file
// systems that keep owners (ext2, memfs). It sits below kernel/vfs so that
// file systems and the VFS can both use it. Never holds policy: who may do
// what is decided in kernel/proc. Docs: docs/04-plan/waves/WAVE-19-unix.md.
package fsattr

import (
	"io/fs"
	"time"
)

// Attr is a file's Unix attributes, as stat(2) reports them. The mode
// itself (type, permissions, set-uid, set-gid, sticky) is fs.FileInfo.Mode().
type Attr struct {
	UID, GID uint32
	Nlink    uint32 // names (and, for a directory, "." and its children's "..")
	Ino      uint64 // unique within the file system
	Atime    time.Time
	Ctime    time.Time // the inode's last change, not its creation
}

// Of is info's Attr, and whether the file system gave one. Without one the
// owner is root's.
func Of(info fs.FileInfo) (Attr, bool) {
	a, ok := info.Sys().(Attr)
	return a, ok
}

// New is what a file is created with: its owner, and its mode (permission,
// set-uid, set-gid and sticky bits; the umask already applied).
type New struct {
	UID, GID uint32
	Mode     fs.FileMode
	Mtime    time.Time
}

// Keep, as Chown's uid or gid, leaves that one as it is.
const Keep = ^uint32(0)

// SpecialBits are the mode bits besides the permissions that Chmod sets.
const SpecialBits = fs.ModeSetuid | fs.ModeSetgid | fs.ModeSticky

// Owner is a file system that keeps owners. Creating calls take the new
// file's New (an existing file keeps its own owner and mode: WriteFileAs
// then replaces only its data); Chmod and Chown change them ("." is the
// file system's root). The VFS uses them when a file system has them.
type Owner interface {
	WriteFileAs(name string, data []byte, n New) error
	MkdirAs(name string, n New) error
	MakeSocketAs(name string, n New) error
	Chmod(name string, mode fs.FileMode) error
	Chown(name string, uid, gid uint32) error
}

// Linker is a file system that holds links (W19-S05: ext2, memfs). Symlink
// makes name a symbolic link to target, owned and moded as n says; Link
// gives oldname's file the name newname too (not a directory: EPERM; an
// existing newname: EEXIST); Lchown is Chown of a link itself.
type Linker interface {
	Symlink(target, name string, n New) error
	Link(oldname, newname string) error
	Lchown(name string, uid, gid uint32) error
}

// Space is a file system's size in bytes (statfs, df; W23-S03): the whole,
// what is free, and what files use. A file system only memory limits
// (a writable memfs) reports Total 0, and the kernel sizes it as Linux
// sizes tmpfs.
type Space struct{ Total, Free, Used uint64 }

// Sizer is a file system that knows its Space; ReadOnly says whether it
// takes writes at all (the mount table's ro or rw).
type Sizer interface {
	Space() Space
	ReadOnly() bool
}
