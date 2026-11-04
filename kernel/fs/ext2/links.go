package ext2

// links.go - making links (W19-S05; S01 reads them): Symlink stores a
// target shorter than the block pointers (60 bytes) in the pointers
// themselves (a fast link, with no block), a longer one in a data block;
// Link adds a directory entry for an existing inode and counts it; Lchown
// changes a link's own owner. A directory never gets a second name.
// Docs: docs/04-plan/waves/WAVE-19-unix.md.

import (
	"io/fs"
	"syscall"

	"burrow/kernel/fsattr"
)

// Symlink makes name a symbolic link to target (mode 0777, as Linux's).
func (f *FS) Symlink(target, name string, n fsattr.New) error {
	return f.change("symlink", name, func() error {
		dir, base, err := f.parentOf(name)
		if err != nil {
			return err
		}
		if len(target) == 0 || int64(len(target)) >= f.sb.blockSize {
			return syscall.ENAMETOOLONG
		}
		n.Mode = 0o777
		return f.create(dir, base, modeLink, n, func(in *inode) error {
			if len(target) < fastLinkMax {
				copy(in.raw[:], target)
				in.size = int64(len(target))
				return nil
			}
			return f.setData(in, []byte(target))
		})
	})
}

// Link gives oldname's inode the name newname too.
func (f *FS) Link(oldname, newname string) error {
	return f.change("link", newname, func() error {
		in, err := f.walk(oldname, false)
		if err != nil {
			return err
		}
		if in.isDir() {
			return syscall.EPERM
		}
		dir, base, err := f.parentOf(newname)
		if err != nil {
			return err
		}
		if _, err := f.child(dir, base); err == nil {
			return fs.ErrExist
		}
		in.links++
		in.ctime = f.stamp()
		if err := f.putInode(in, false); err != nil {
			return err
		}
		return f.addEntry(dir, base, in)
	})
}

// Lchown sets name's own owner and group, a link's included.
func (f *FS) Lchown(name string, uid, gid uint32) error {
	return f.change("lchown", name, func() error {
		in, err := f.walk(name, false)
		if err != nil {
			return err
		}
		return f.setOwner(in, uid, gid)
	})
}

func (f *FS) setOwner(in *inode, uid, gid uint32) error {
	if uid != fsattr.Keep {
		in.uid = uid
	}
	if gid != fsattr.Keep {
		in.gid = gid
	}
	in.ctime = f.stamp()
	return f.putInode(in, false)
}
