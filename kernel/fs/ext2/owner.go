package ext2

// owner.go - owners and modes (W19-S04): fsattr.Owner (create with an
// owner, Chmod, Chown), and the vfs.Writable calls of S02 as root's.
// Chmod and Chown follow a symbolic link to its target, as chmod(2) and
// chown(2) do. Never decides who may change what: kernel/proc does.
// Docs: docs/04-plan/waves/WAVE-19-unix.md.

import (
	"io/fs"
	"time"

	"burrow/kernel/fsattr"
)

// unixMode is the permission, set-id and sticky bits of m as i_mode bits.
func unixMode(m fs.FileMode) uint16 {
	b := uint16(m.Perm())
	if m&fs.ModeSetuid != 0 {
		b |= modeSetuid
	}
	if m&fs.ModeSetgid != 0 {
		b |= modeSetgid
	}
	if m&fs.ModeSticky != 0 {
		b |= modeSticky
	}
	return b
}

func rootOwned(mode fs.FileMode, mtime time.Time) fsattr.New {
	return fsattr.New{Mode: mode, Mtime: mtime}
}

// WriteFile is WriteFileAs for root.
func (f *FS) WriteFile(name string, data []byte, mode fs.FileMode, mtime time.Time) error {
	return f.WriteFileAs(name, data, rootOwned(mode, mtime))
}

// Mkdir is MkdirAs for root.
func (f *FS) Mkdir(name string, mode fs.FileMode, mtime time.Time) error {
	return f.MkdirAs(name, rootOwned(mode, mtime))
}

// MakeSocket is MakeSocketAs for root, mode 0755.
func (f *FS) MakeSocket(name string, mtime time.Time) error {
	return f.MakeSocketAs(name, rootOwned(0o755, mtime))
}

// Chmod sets name's permission, set-id and sticky bits.
func (f *FS) Chmod(name string, mode fs.FileMode) error {
	return f.change("chmod", name, func() error {
		in, err := f.walk(name, true)
		if err != nil {
			return err
		}
		in.mode = in.mode&modeType | unixMode(mode)
		in.ctime = f.stamp()
		return f.putInode(in, false)
	})
}

// Chown sets name's owner and group; fsattr.Keep leaves one as it is.
func (f *FS) Chown(name string, uid, gid uint32) error {
	return f.change("chown", name, func() error {
		in, err := f.walk(name, true)
		if err != nil {
			return err
		}
		if uid != fsattr.Keep {
			in.uid = uid
		}
		if gid != fsattr.Keep {
			in.gid = gid
		}
		in.ctime = f.stamp()
		return f.putInode(in, false)
	})
}
