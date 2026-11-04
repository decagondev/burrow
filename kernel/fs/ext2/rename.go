package ext2

// rename.go - Rename (W19-S02), with Linux's rules: a file replaces a file,
// a directory replaces an empty directory, a directory cannot move inside
// itself, renaming a name onto another name of the same inode does nothing.
// The new name is linked before the old one goes, so a crash in between
// leaves two names, not none; a directory that moves gets a new "..", and
// the link counts of both parents follow. Never crosses volumes: the VFS
// says EXDEV first. Docs: WAVE-19-unix.md.

import (
	"errors"
	"io/fs"
	"strings"
	"syscall"
)

// Rename moves oldname to newname.
func (f *FS) Rename(oldname, newname string) error {
	return f.change("rename", oldname, func() error {
		sp, sbase, err := f.parentOf(oldname)
		if err != nil {
			return err
		}
		src, err := f.child(sp, sbase)
		if err != nil {
			return err
		}
		dp, dbase, err := f.parentOf(newname)
		if err != nil {
			return err
		}
		if dp.ino == sp.ino {
			dp = sp // one inode, one copy: both changes must land
		}
		if oldname == newname {
			return nil
		}
		if src.isDir() && strings.HasPrefix(newname+"/", oldname+"/") {
			return syscall.EINVAL
		}
		dst, err := f.child(dp, dbase)
		switch {
		case err == nil:
			if dst.ino == src.ino {
				return nil
			}
			if err := f.replaceable(src, dst); err != nil {
				return err
			}
			if err := f.unlink(dp, dbase, dst); err != nil {
				return err
			}
		case !errors.Is(err, fs.ErrNotExist):
			return err
		}
		return f.move(sp, sbase, src, dp, dbase)
	})
}

func (f *FS) replaceable(src, dst *inode) error {
	switch {
	case src.isDir() && !dst.isDir():
		return syscall.ENOTDIR
	case !src.isDir() && dst.isDir():
		return syscall.EISDIR
	}
	return nil // a non-empty directory: unlink says ENOTEMPTY
}

// move links src as dbase in dp, then unlinks sbase from sp.
func (f *FS) move(sp *inode, sbase string, src, dp *inode, dbase string) error {
	moved := src.isDir() && dp != sp
	if moved {
		dp.links++ // src's ".." will name it; addEntry writes dp
	}
	if err := f.addEntry(dp, dbase, src); err != nil {
		if moved {
			dp.links--
		}
		return err
	}
	if moved {
		sp.links-- // removeEntry writes sp
		if err := f.setDotDot(src, dp.ino); err != nil {
			return err
		}
	}
	if err := f.removeEntry(sp, sbase); err != nil {
		return err
	}
	src.ctime = f.stamp()
	return f.putInode(src, false)
}
