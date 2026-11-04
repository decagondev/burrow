package memfs

// owners.go - owners, modes and inode numbers (W19-S04): fsattr.Owner, so
// /tmp and the initrd (tarfs) keep who owns what. A new node gets the
// owner and mode it is created with; a rewrite of an existing file keeps
// its node, owner and mode and replaces only the data. Chmod and Chown may
// change "." (the tree's root: /tmp's own mode). Never decides who may.
// Docs: docs/04-plan/waves/WAVE-19-unix.md.

import (
	"io/fs"
	"syscall"
	"time"

	"burrow/kernel/fsattr"
)

const keptMode = fs.ModePerm | fsattr.SpecialBits

func (f *FS) newNode(base string, typ fs.FileMode, n fsattr.New) *node {
	f.lastIno++
	return &node{name: base, mode: typ | n.Mode&keptMode, modTime: n.Mtime, uid: n.UID, gid: n.GID,
		ino: f.lastIno, ctime: n.Mtime, links: 1}
}

func (n *node) attr() fsattr.Attr {
	links := uint32(max(n.links, 1))
	if n.mode.IsDir() {
		links = 2
		for _, c := range n.children {
			if c.mode.IsDir() {
				links++
			}
		}
	}
	return fsattr.Attr{UID: n.uid, GID: n.gid, Nlink: links, Ino: n.ino, Atime: n.modTime, Ctime: n.ctime}
}

// WriteFileAs creates name with n's owner and mode, or replaces the data of
// the file already there.
func (f *FS) WriteFileAs(name string, data []byte, n fsattr.New) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	p, base, err := f.parent("write", name)
	if err != nil {
		return err
	}
	if old, ok := p.children[base]; ok {
		if old.mode.IsDir() {
			return &fs.PathError{Op: "write", Path: name, Err: fs.ErrExist}
		}
		old.data, old.modTime, old.ctime = data, n.Mtime, n.Mtime
		return nil
	}
	c := f.newNode(base, 0, n)
	c.data = data
	p.children[base] = c
	return nil
}

// MkdirAs creates one directory with n's owner and mode.
func (f *FS) MkdirAs(name string, n fsattr.New) error {
	return f.add("mkdir", name, fs.ModeDir, n)
}

// MakeSocketAs creates a socket node with n's owner and mode.
func (f *FS) MakeSocketAs(name string, n fsattr.New) error {
	return f.add("bind", name, fs.ModeSocket, n)
}

func (f *FS) add(op, name string, typ fs.FileMode, n fsattr.New) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	p, base, err := f.parent(op, name)
	if err != nil {
		return err
	}
	if _, ok := p.children[base]; ok {
		return &fs.PathError{Op: op, Path: name, Err: fs.ErrExist}
	}
	c := f.newNode(base, typ, n)
	if typ == fs.ModeDir {
		c.children = map[string]*node{}
	}
	p.children[base] = c
	return nil
}

// changeNode runs fn on name's node ("." is the root) under the write lock.
func (f *FS) changeNode(op, name string, fn func(*node)) error {
	if !fs.ValidPath(name) {
		return &fs.PathError{Op: op, Path: name, Err: fs.ErrInvalid}
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.readOnly {
		return &fs.PathError{Op: op, Path: name, Err: syscall.EROFS}
	}
	n, ok := f.lookup(name)
	if !ok {
		return &fs.PathError{Op: op, Path: name, Err: fs.ErrNotExist}
	}
	fn(n)
	n.ctime = time.Now()
	return nil
}

// Chmod sets name's permission, set-id and sticky bits.
func (f *FS) Chmod(name string, mode fs.FileMode) error {
	return f.changeNode("chmod", name, func(n *node) { n.mode = n.mode.Type() | mode&keptMode })
}

// Chown sets name's owner and group; fsattr.Keep leaves one.
func (f *FS) Chown(name string, uid, gid uint32) error {
	return f.changeNode("chown", name, func(n *node) {
		if uid != fsattr.Keep {
			n.uid = uid
		}
		if gid != fsattr.Keep {
			n.gid = gid
		}
	})
}
