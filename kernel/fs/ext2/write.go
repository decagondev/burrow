package ext2

// write.go - a writable mount (W19-S02): vfs.Writable (WriteFile, Mkdir,
// Remove), MakeSocket and Rename (rename.go), and since S04 fsattr.Owner
// (owner.go: the same with an owner, Chmod, Chown). Each operation runs under
// the write lock and ends with a commit: the bitmaps it changed, the group
// descriptors and the superblock (free counts, write time) are written and
// the disk flushed, so the volume is consistent between operations. There
// is no journal: a crash inside one can leave work for e2fsck (ADR-0010).
// New files get the owner and mode the caller gives (W19-S04). Never
// writes when mounted with Mount, or when the volume has a read-only-
// compatible feature it does not know. Docs: WAVE-19-unix.md.

import (
	"burrow/kernel/fsattr"
	"errors"
	"io"
	"io/fs"
	"path"
	"syscall"
)

// Disk is what a writable volume lives on.
type Disk interface {
	io.ReaderAt
	io.WriterAt
	Flush() error
}

// MountRW mounts the volume on d for reading and writing.
func MountRW(d Disk) (*FS, error) {
	if d == nil {
		return nil, errors.New("ext2: no disk")
	}
	f, err := Mount(d)
	if err != nil {
		return nil, err
	}
	f.disk = d
	return f, nil
}

// Writable says whether writes can work: MountRW, and nothing unknown.
func (f *FS) Writable() bool { return f.disk != nil && !f.ReadOnlyFeatures }

// commit writes what the last operation changed besides inodes and data.
func (f *FS) commit() error {
	for blk := range f.dirty {
		if _, err := f.disk.WriteAt(f.bitmaps[blk], int64(blk)*f.sb.blockSize); err != nil {
			return err
		}
		delete(f.dirty, blk)
	}
	if _, err := f.disk.WriteAt(f.gdt, int64(f.sb.firstDataBlock+1)*f.sb.blockSize); err != nil {
		return err
	}
	le.PutUint32(f.super[sWtime:], f.stamp())
	if _, err := f.disk.WriteAt(f.super, superOffset); err != nil {
		return err
	}
	return f.disk.Flush()
}

// change runs op under the write lock and commits, as one operation.
func (f *FS) change(opName, name string, op func() error) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if !f.Writable() {
		return &fs.PathError{Op: opName, Path: name, Err: fs.ErrPermission}
	}
	err := op()
	if cerr := f.commit(); err == nil {
		err = cerr
	}
	if err != nil {
		return pathErr(opName, name, err)
	}
	return nil
}

// parentOf is the directory that holds name, and name's last element.
func (f *FS) parentOf(name string) (*inode, string, error) {
	if !fs.ValidPath(name) || name == "." {
		return nil, "", fs.ErrInvalid
	}
	dir, base := path.Split(name)
	if len(base) > maxNameLen {
		return nil, "", syscall.ENAMETOOLONG
	}
	if dir == "" {
		dir = "."
	}
	d, err := f.walk(path.Clean(dir), true)
	switch {
	case err != nil:
		return nil, "", err
	case !d.isDir():
		return nil, "", syscall.ENOTDIR
	}
	return d, base, nil
}

// create makes a new inode of type typ, with n's owner and mode, linked as
// base in dir; fill gives it data before it is written and linked.
func (f *FS) create(dir *inode, base string, typ uint16, n fsattr.New, fill func(*inode) error) error {
	if _, err := f.child(dir, base); err == nil {
		return fs.ErrExist
	}
	isDir := typ == modeDir
	ino, err := f.allocInode(isDir, dir.ino)
	if err != nil {
		return err
	}
	t := uint32(n.Mtime.Unix())
	in := &inode{ino: ino, mode: typ | unixMode(n.Mode), uid: n.UID, gid: n.GID, links: 1, atime: t, ctime: f.stamp(), mtime: t}
	if fill != nil {
		err = fill(in)
	}
	if err == nil {
		err = f.putInode(in, true)
	}
	if err == nil {
		err = f.addEntry(dir, base, in)
	}
	if err != nil {
		f.freeData(in)
		f.freeInode(ino, isDir)
	}
	return err
}

// WriteFileAs creates name with n's owner and mode, or replaces its
// contents (keeping its inode, owner and mode, so every hard link sees the
// new contents).
func (f *FS) WriteFileAs(name string, data []byte, n fsattr.New) error {
	return f.change("write", name, func() error {
		dir, base, err := f.parentOf(name)
		if err != nil {
			return err
		}
		in, err := f.walk(name, true)
		switch {
		case errors.Is(err, fs.ErrNotExist):
			if _, lerr := f.child(dir, base); lerr == nil {
				return fs.ErrNotExist // a link to nothing
			}
			return f.create(dir, base, modeReg, n, func(in *inode) error {
				return f.setData(in, data)
			})
		case err != nil:
			return err
		case in.isDir():
			return syscall.EISDIR
		case !in.isReg():
			return fs.ErrPermission
		}
		err = f.setData(in, data)
		in.mtime, in.ctime = uint32(n.Mtime.Unix()), f.stamp()
		if perr := f.putInode(in, false); err == nil {
			err = perr
		}
		return err
	})
}

// MkdirAs makes a directory with "." and "..", owned and moded as n says.
func (f *FS) MkdirAs(name string, n fsattr.New) error {
	return f.change("mkdir", name, func() error {
		dir, base, err := f.parentOf(name)
		if err != nil {
			return err
		}
		dir.links++
		err = f.create(dir, base, modeDir, n, func(in *inode) error {
			in.links = 2
			return f.initDir(in, dir.ino)
		})
		if err != nil {
			dir.links--
		}
		return err
	})
}

// initDir gives a new directory its first block: "." and "..".
func (f *FS) initDir(in *inode, parent uint32) error {
	bs := f.sb.blockSize
	t := newTree(f, in, f.goal(in))
	blk, err := t.place(0)
	if err != nil {
		t.undo()
		return err
	}
	b := make([]byte, bs)
	f.putEntry(b, in.ino, recLen(1), ".", ftDir)
	f.putEntry(b[recLen(1):], parent, int(bs)-recLen(1), "..", ftDir)
	if err := f.putBlock(b, blk); err != nil {
		t.undo()
		return err
	}
	in.size, in.blocks512 = bs, uint32(bs/512)
	return nil
}

// MakeSocketAs makes a socket node (a bound Unix socket's name, W14-S07).
func (f *FS) MakeSocketAs(name string, n fsattr.New) error {
	return f.change("bind", name, func() error {
		dir, base, err := f.parentOf(name)
		if err != nil {
			return err
		}
		return f.create(dir, base, modeSock, n, nil)
	})
}

// Remove deletes a file, a link or an empty directory.
func (f *FS) Remove(name string) error {
	return f.change("remove", name, func() error {
		dir, base, err := f.parentOf(name)
		if err != nil {
			return err
		}
		in, err := f.child(dir, base)
		if err != nil {
			return err
		}
		return f.unlink(dir, base, in)
	})
}

// unlink takes base (which is in) out of dir and lets in go when nothing
// names it any more.
func (f *FS) unlink(dir *inode, base string, in *inode) error {
	if in.isDir() {
		ents, err := f.readDir(in)
		if err != nil {
			return err
		}
		if len(ents) > 0 {
			return syscall.ENOTEMPTY
		}
		dir.links-- // its ".."; removeEntry writes dir
		in.links = 1
	}
	if err := f.removeEntry(dir, base); err != nil {
		return err
	}
	if in.links--; in.links > 0 {
		in.ctime = f.stamp()
		return f.putInode(in, false)
	}
	if err := f.freeData(in); err != nil {
		return err
	}
	in.dtime, in.size = f.stamp(), 0
	if err := f.putInode(in, false); err != nil {
		return err
	}
	return f.freeInode(in.ino, in.isDir())
}
