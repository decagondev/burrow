package vfs

// attr.go - what a file system knows about a file beyond io/fs (W19-S01):
// its owner, its links and its inode, given as fs.FileInfo.Sys(). The mode
// itself (type, permissions, set-uid, set-gid, sticky) stays in
// fs.FileInfo.Mode(). File systems with no place for owners (FAT, devfs)
// give none or one for the whole mount; kernel/proc treats a missing Attr
// as root's. Never holds policy: who may do what is decided in kernel/proc.
// Docs: docs/04-plan/waves/WAVE-19-unix.md.

import (
	"io/fs"
	"time"
)

// Attr is a file's Unix attributes, as stat(2) reports them.
type Attr struct {
	UID, GID uint32
	Nlink    uint32 // names (and, for a directory, "." and its children's "..")
	Ino      uint64 // unique within the file system
	Atime    time.Time
	Ctime    time.Time // the inode's last change, not its creation
}

// AttrOf is info's Attr, and whether the file system gave one.
func AttrOf(info fs.FileInfo) (Attr, bool) {
	a, ok := info.Sys().(Attr)
	return a, ok
}
