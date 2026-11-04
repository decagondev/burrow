package memfs

// links.go - symbolic and hard links (W19-S05): a symbolic link is a node
// holding its target as data; a hard link is the same node under a second
// name, with a link count, so a rewrite through one name is seen through
// every name. The tree is an fs.ReadLinkFS: Open follows links inside the
// tree (an absolute target from the tree's root, at most 40 links), Lstat
// and ReadLink do not; the VFS resolves links across mounts (vfs.Resolve)
// and hands the tree link-free paths. Never lets a directory get a second
// name. Docs: docs/04-plan/waves/WAVE-19-unix.md.

import (
	"io/fs"
	"path"
	"strings"
	"syscall"

	"burrow/kernel/fsattr"
)

const maxLinks = 40

// follow is the node name leads to, links followed; f.mu is held.
func (f *FS) follow(name string) (*node, error) {
	todo := strings.Split(name, "/")
	stack := []*node{f.root}
	links := 0
	for len(todo) > 0 {
		part := todo[0]
		todo = todo[1:]
		cur := stack[len(stack)-1]
		switch {
		case part == "" || part == ".":
			continue
		case !cur.mode.IsDir():
			return nil, fs.ErrNotExist
		case part == "..":
			if len(stack) > 1 {
				stack = stack[:len(stack)-1]
			}
			continue
		}
		c, ok := cur.children[part]
		if !ok {
			return nil, fs.ErrNotExist
		}
		if c.mode&fs.ModeSymlink != 0 {
			if links++; links > maxLinks {
				return nil, syscall.ELOOP
			}
			target := string(c.data)
			if strings.HasPrefix(target, "/") {
				stack = stack[:1]
			}
			todo = append(strings.Split(target, "/"), todo...)
			continue
		}
		stack = append(stack, c)
	}
	return stack[len(stack)-1], nil
}

// Lstat implements fs.ReadLinkFS: a final link is described itself.
func (f *FS) Lstat(name string) (fs.FileInfo, error) {
	if !fs.ValidPath(name) {
		return nil, &fs.PathError{Op: "lstat", Path: name, Err: fs.ErrInvalid}
	}
	f.mu.RLock()
	defer f.mu.RUnlock()
	n, ok := f.lookup(name)
	if !ok {
		return nil, &fs.PathError{Op: "lstat", Path: name, Err: fs.ErrNotExist}
	}
	i := n.info()
	if name != "." {
		i.name = base(name)
	}
	return i, nil
}

// ReadLink implements fs.ReadLinkFS.
func (f *FS) ReadLink(name string) (string, error) {
	if !fs.ValidPath(name) {
		return "", &fs.PathError{Op: "readlink", Path: name, Err: fs.ErrInvalid}
	}
	f.mu.RLock()
	defer f.mu.RUnlock()
	n, ok := f.lookup(name)
	switch {
	case !ok:
		return "", &fs.PathError{Op: "readlink", Path: name, Err: fs.ErrNotExist}
	case n.mode&fs.ModeSymlink == 0:
		return "", &fs.PathError{Op: "readlink", Path: name, Err: fs.ErrInvalid}
	}
	return string(n.data), nil
}

// Symlink makes name a link to target (mode 0777, as Linux's links).
func (f *FS) Symlink(target, name string, n fsattr.New) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	p, base, err := f.parent("symlink", name)
	if err != nil {
		return err
	}
	if _, ok := p.children[base]; ok {
		return &fs.PathError{Op: "symlink", Path: name, Err: fs.ErrExist}
	}
	n.Mode = 0o777
	c := f.newNode(base, fs.ModeSymlink, n)
	c.data = []byte(target)
	p.children[base] = c
	return nil
}

// Link gives oldname's node the name newname too.
func (f *FS) Link(oldname, newname string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if !fs.ValidPath(oldname) {
		return &fs.PathError{Op: "link", Path: oldname, Err: fs.ErrInvalid}
	}
	n, ok := f.lookup(oldname)
	switch {
	case !ok:
		return &fs.PathError{Op: "link", Path: oldname, Err: fs.ErrNotExist}
	case n.mode.IsDir():
		return &fs.PathError{Op: "link", Path: oldname, Err: syscall.EPERM}
	}
	p, base, err := f.parent("link", newname)
	if err != nil {
		return err
	}
	if _, ok := p.children[base]; ok {
		return &fs.PathError{Op: "link", Path: newname, Err: fs.ErrExist}
	}
	n.links++
	p.children[base] = n
	return nil
}

// Lchown is Chown of name itself: memfs's Chown never follows a final link
// either (the VFS resolves before it asks), so they are the same.
func (f *FS) Lchown(name string, uid, gid uint32) error { return f.Chown(name, uid, gid) }

// base is name's last element, for an info under the name it was opened by.
func base(name string) string { return path.Base(name) }
