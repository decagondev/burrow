package vfs

// walk.go - links through the VFS (W19-S05): Lstat and ReadLink (the VFS
// is an fs.ReadLinkFS), Symlink, Link and Lchown handed to the mount that
// holds the name, and Resolve, the one path walker: a path is taken a
// component at a time, symbolic links are followed (at most 40, then
// ELOOP; the last one only when asked), ".." goes back along the path as
// walked, not as written, and mounts are crossed. Everything else in the
// VFS takes the link-free paths Resolve gives. A file system without links
// (FAT, tarfs before W19, devfs) has none to follow. Since W23-S01 a file
// system's links may read differently for each process (procfs's self):
// ResolveAs and ReadLinkAs carry the Caller to it. Never decides who may
// pass: the visit callback does (kernel/proc checks search permission).
// Docs: docs/04-plan/waves/WAVE-19-unix.md, WAVE-23-proc-virtio.md.

import (
	"errors"
	"io/fs"
	"strings"
	"syscall"

	"burrow/kernel/fsattr"
)

const maxLinks = 40 // as Linux

// Caller is the process (and thread) a path is walked for; the zero Caller
// is the kernel itself.
type Caller struct{ PID, TID int }

// CallerLinks is a file system whose links read differently for each
// caller (procfs: self is the caller's pid). ReadLink without one is
// ReadLinkAs for the zero Caller.
type CallerLinks interface {
	ReadLinkAs(name string, c Caller) (string, error)
}

// Lstat describes name; a final symbolic link is described itself.
func (v *VFS) Lstat(name string) (fs.FileInfo, error) {
	if !fs.ValidPath(name) {
		return nil, &fs.PathError{Op: "lstat", Path: name, Err: fs.ErrInvalid}
	}
	m, rel, ok := v.resolve(name)
	if rl, isRL := m.fsys.(fs.ReadLinkFS); ok && isRL && rel != "." {
		info, err := rl.Lstat(rel)
		if !errors.Is(err, fs.ErrNotExist) || len(v.below(name)) == 0 {
			return info, wrap("lstat", name, err)
		}
	}
	return fs.Stat(v, name) // a mount's root, or a directory only mounts make
}

// ReadLink is the target of the symbolic link name.
func (v *VFS) ReadLink(name string) (string, error) { return v.ReadLinkAs(name, Caller{}) }

// ReadLinkAs is the target of the symbolic link name, as c sees it.
func (v *VFS) ReadLinkAs(name string, c Caller) (string, error) {
	if !fs.ValidPath(name) {
		return "", &fs.PathError{Op: "readlink", Path: name, Err: fs.ErrInvalid}
	}
	m, rel, ok := v.resolve(name)
	if cl, isCL := m.fsys.(CallerLinks); ok && isCL && rel != "." {
		target, err := cl.ReadLinkAs(rel, c)
		return target, wrap("readlink", name, err)
	}
	if rl, isRL := m.fsys.(fs.ReadLinkFS); ok && isRL && rel != "." {
		target, err := rl.ReadLink(rel)
		return target, wrap("readlink", name, err)
	}
	if _, err := fs.Stat(v, name); err != nil {
		return "", err
	}
	return "", &fs.PathError{Op: "readlink", Path: name, Err: fs.ErrInvalid} // not a link
}

// linker is the fsattr.Linker holding name, and name inside it.
func (v *VFS) linker(op, name string) (fsattr.Linker, string, error) {
	w, rel, err := v.writable(op, name)
	if err != nil {
		return nil, "", err
	}
	l, ok := w.(fsattr.Linker)
	if !ok {
		return nil, "", &fs.PathError{Op: op, Path: name, Err: syscall.EPERM} // FAT: no links, as vfat
	}
	return l, rel, nil
}

// Symlink makes name a symbolic link to target.
func (v *VFS) Symlink(target, name string, n NewFile) error {
	l, rel, err := v.linker("symlink", name)
	if err != nil {
		return err
	}
	return wrap("symlink", name, l.Symlink(target, rel, n))
}

// Link gives oldname's file the name newname too, inside one mount (EXDEV
// across mounts, as Linux).
func (v *VFS) Link(oldname, newname string) error {
	l, from, err := v.linker("link", oldname)
	if err != nil {
		return err
	}
	l2, to, err := v.linker("link", newname)
	if err != nil {
		return err
	}
	if l != l2 {
		return &fs.PathError{Op: "link", Path: newname, Err: syscall.EXDEV}
	}
	return wrap("link", oldname, l.Link(from, to))
}

// Lchown sets the owner of name itself, a symbolic link included.
func (v *VFS) Lchown(name string, uid, gid uint32) error {
	l, rel, err := v.linker("lchown", name)
	if err != nil {
		return err
	}
	return wrap("lchown", name, l.Lchown(rel, uid, gid))
}

// Resolve turns raw (a path from the root, as the caller joined it: it may
// hold "", "." and ".." parts and symbolic links) into a link-free io/fs
// path. follow says whether a final link is followed; a trailing slash
// follows it too and asks for a directory. A missing final component is
// not an error (the caller may create it, or say ENOENT); a missing one
// before it is. visit, if not nil, sees every directory the walk looks
// into (the root first) and may refuse with an error.
func (v *VFS) Resolve(raw string, follow bool, visit func(dir string, info fs.FileInfo) error) (string, error) {
	return v.ResolveAs(raw, follow, Caller{}, visit)
}

// ResolveAs is Resolve for c: links that depend on the caller read as c's.
func (v *VFS) ResolveAs(raw string, follow bool, c Caller, visit func(dir string, info fs.FileInfo) error) (string, error) {
	todo := strings.Split(raw, "/")
	trailing := strings.HasSuffix(raw, "/")
	var done []string
	cur, links := ".", 0
	curInfo, err := fs.Stat(v, ".")
	if err != nil {
		return "", err
	}
	for len(todo) > 0 {
		part := todo[0]
		todo = todo[1:]
		if part == "" || part == "." {
			continue
		}
		if !curInfo.IsDir() {
			return "", syscall.ENOTDIR
		}
		if visit != nil {
			if err := visit(cur, curInfo); err != nil {
				return "", err
			}
		}
		if part == ".." {
			if len(done) > 0 {
				done = done[:len(done)-1]
			}
			if cur, curInfo, err = v.at(done); err != nil {
				return "", err
			}
			continue
		}
		next := join(append(done, part))
		info, err := v.Lstat(next)
		last := rest(todo)
		switch {
		case errors.Is(err, fs.ErrNotExist) && last:
			return next, nil
		case err != nil:
			return "", err
		}
		if info.Mode()&fs.ModeSymlink != 0 && (!last || follow || trailing) {
			if links++; links > maxLinks {
				return "", syscall.ELOOP
			}
			target, err := v.ReadLinkAs(next, c)
			if err != nil {
				return "", err
			}
			if strings.HasPrefix(target, "/") {
				done = nil
			}
			todo = append(strings.Split(target, "/"), todo...)
			if cur, curInfo, err = v.at(done); err != nil {
				return "", err
			}
			continue
		}
		done, cur, curInfo = append(done, part), next, info
	}
	if trailing && !curInfo.IsDir() {
		return "", syscall.ENOTDIR
	}
	return cur, nil
}

// at is the path of parts and what is there.
func (v *VFS) at(parts []string) (string, fs.FileInfo, error) {
	p := join(parts)
	info, err := fs.Stat(v, p)
	return p, info, err
}

func join(parts []string) string {
	if len(parts) == 0 {
		return "."
	}
	return strings.Join(parts, "/")
}

// rest says whether nothing but "" and "." parts is left.
func rest(todo []string) bool {
	for _, p := range todo {
		if p != "" && p != "." {
			return false
		}
	}
	return true
}
