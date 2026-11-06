package procfs

// procfs.go - the tree: the root (a directory per pid, the system files,
// self, sys/kernel), each pid's directory and files, and the io/fs face
// (Open, Stat, Lstat, ReadLink, and ReadLinkAs for links that depend on
// who asks). A name that stops existing between a listing and an open
// (a process exited) is ErrNotExist, never a crash. Docs: WAVE-23-proc-virtio.md.

import (
	"io/fs"
	"sort"
	"strconv"
	"strings"

	"burrow/kernel/fsattr"
	"burrow/kernel/vfs"
)

// FS is /proc over a Source.
type FS struct {
	src Source
	st  Static
}

// New is /proc for src.
func New(src Source, st Static) *FS { return &FS{src: src, st: st} }

// node is one name in the tree: a file (data), a directory (list) or a
// symbolic link (link).
type node struct {
	name string
	mode fs.FileMode
	attr fsattr.Attr
	data func() []byte
	list func() []string
	link func(c vfs.Caller) (string, error)
}

const (
	dirMode  = fs.ModeDir | 0o555
	fileMode = 0o444
	linkMode = fs.ModeSymlink | 0o777
	inoRoot  = 1
	inoSys   = 100 // sys/, sys/kernel/ and its files follow
)

// lookup is the node at name (an io/fs path); a link inside the path is
// followed as the zero caller sees it.
func (f *FS) lookup(name string) (node, error) {
	if name == "." {
		return f.root(), nil
	}
	parts := strings.Split(name, "/")
	if parts[0] == "self" {
		if len(parts) == 1 {
			return f.self(), nil
		}
		parts[0], _ = f.self().link(vfs.Caller{})
	}
	if parts[0] == "sys" {
		return f.sysNode(parts[1:])
	}
	if pid, err := strconv.Atoi(parts[0]); err == nil && pid > 0 && parts[0] == strconv.Itoa(pid) {
		return f.pidNode(pid, parts[1:])
	}
	if len(parts) == 1 && parts[0] == "mounts" { // as Linux: a link to the caller's own
		return f.link("mounts", fsattr.Attr{Ino: inoRoot + 1001}, 0o777,
			func(vfs.Caller) (string, error) { return "self/mounts", nil }), nil
	}
	if len(parts) == 1 {
		for i, sf := range systemFiles {
			if sf.name == parts[0] {
				return f.file(sf.name, fileMode, fsattr.Attr{Ino: uint64(2 + i)}, func() []byte { return sf.gen(f) }), nil
			}
		}
	}
	return node{}, fs.ErrNotExist
}

func (f *FS) file(name string, mode fs.FileMode, a fsattr.Attr, data func() []byte) node {
	a.Nlink = 1
	return node{name: name, mode: mode, attr: a, data: data}
}

func (f *FS) dir(name string, a fsattr.Attr, list func() []string) node {
	a.Nlink = 2
	return node{name: name, mode: dirMode, attr: a, list: list}
}

func (f *FS) root() node {
	return f.dir(".", fsattr.Attr{Ino: inoRoot}, func() []string {
		names := []string{"mounts", "self", "sys"}
		for _, sf := range systemFiles {
			names = append(names, sf.name)
		}
		for _, pid := range f.src.Pids() {
			names = append(names, strconv.Itoa(pid))
		}
		return names
	})
}

// self is the caller's own directory; the kernel itself reads as init.
func (f *FS) self() node {
	return node{name: "self", mode: linkMode, attr: fsattr.Attr{Ino: inoRoot + 1000, Nlink: 1},
		link: func(c vfs.Caller) (string, error) {
			if c.PID == 0 {
				return "1", nil
			}
			return strconv.Itoa(c.PID), nil
		}}
}

// pidNode is a pid's directory (rest empty) or one of its files.
func (f *FS) pidNode(pid int, rest []string) (node, error) {
	p, ok := f.src.Process(pid)
	if !ok {
		return node{}, fs.ErrNotExist
	}
	a := fsattr.Attr{UID: p.UID[1], GID: p.GID[1], Ino: uint64(pid+1) << 8}
	switch len(rest) {
	case 0:
		return f.dir(strconv.Itoa(pid), a, func() []string {
			names := []string{"task", "fd", "fdinfo", "cwd", "root"}
			if p.Exe != "" {
				names = append(names, "exe")
			}
			for _, pf := range pidFiles {
				names = append(names, pf.name)
			}
			return names
		}), nil
	}
	switch rest[0] {
	case "task":
		return f.taskNode(&p, a, rest[1:])
	case "fd", "fdinfo":
		return f.fdNode(&p, a, rest[0], rest[1:])
	}
	if l, ok := f.procLink(&p, a, rest[0]); ok && len(rest) == 1 {
		return l, nil
	}
	switch len(rest) {
	case 1:
		for i, pf := range pidFiles {
			if pf.name == rest[0] {
				a.Ino += uint64(i + 1)
				return f.file(pf.name, pf.mode, a, func() []byte { return pf.gen(f, &p) }), nil
			}
		}
	}
	return node{}, fs.ErrNotExist
}

// sysNode is sys/, sys/kernel/ or one of its files.
func (f *FS) sysNode(rest []string) (node, error) {
	switch {
	case len(rest) == 0:
		return f.dir("sys", fsattr.Attr{Ino: inoSys}, func() []string { return []string{"kernel"} }), nil
	case rest[0] != "kernel":
	case len(rest) == 1:
		return f.dir("kernel", fsattr.Attr{Ino: inoSys + 1}, func() []string {
			names := make([]string, len(kernelFiles))
			for i, kf := range kernelFiles {
				names[i] = kf.name
			}
			return names
		}), nil
	case len(rest) == 2:
		for i, kf := range kernelFiles {
			if kf.name == rest[1] {
				return f.file(kf.name, fileMode, fsattr.Attr{Ino: uint64(inoSys + 2 + i)},
					func() []byte { return []byte(kf.gen(f) + "\n") }), nil
			}
		}
	}
	return node{}, fs.ErrNotExist
}

// Open opens name; a link is followed (as the kernel sees it).
func (f *FS) Open(name string) (fs.File, error) {
	if !fs.ValidPath(name) {
		return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrInvalid}
	}
	n, err := f.lookup(name)
	if err == nil && n.link != nil {
		// self and mounts lead inside /proc; a descriptor's or exe's target
		// is elsewhere in the tree, which the VFS's walker follows: opened
		// here, such a link is an empty file
		if target, terr := n.link(vfs.Caller{}); terr == nil && !strings.HasPrefix(target, "/") && !strings.Contains(target, ":") {
			n, err = f.lookup(target)
		} else {
			n.data = func() []byte { return nil }
		}
	}
	if err != nil {
		return nil, &fs.PathError{Op: "open", Path: name, Err: err}
	}
	if n.list != nil {
		return f.openDir(n, name), nil
	}
	return newFile(f, n), nil
}

// Stat describes name, following a link.
func (f *FS) Stat(name string) (fs.FileInfo, error) {
	file, err := f.Open(name)
	if err != nil {
		return nil, &fs.PathError{Op: "stat", Path: name, Err: fs.ErrNotExist}
	}
	defer file.Close()
	return file.Stat()
}

// Lstat describes name, a link itself.
func (f *FS) Lstat(name string) (fs.FileInfo, error) {
	if !fs.ValidPath(name) {
		return nil, &fs.PathError{Op: "lstat", Path: name, Err: fs.ErrInvalid}
	}
	n, err := f.lookup(name)
	if err != nil {
		return nil, &fs.PathError{Op: "lstat", Path: name, Err: err}
	}
	return info{f, n}, nil
}

// ReadLink is the target of the link name, as the kernel sees it.
func (f *FS) ReadLink(name string) (string, error) { return f.ReadLinkAs(name, vfs.Caller{}) }

// ReadLinkAs is the target of the link name, as c sees it.
func (f *FS) ReadLinkAs(name string, c vfs.Caller) (string, error) {
	n, err := f.lookup(name)
	switch {
	case err != nil:
		return "", &fs.PathError{Op: "readlink", Path: name, Err: err}
	case n.link == nil:
		return "", &fs.PathError{Op: "readlink", Path: name, Err: fs.ErrInvalid}
	}
	target, err := n.link(c)
	if err != nil {
		return "", &fs.PathError{Op: "readlink", Path: name, Err: err}
	}
	return target, nil
}

// sorted is names in fs.ReadDir's order.
func sorted(names []string) []string {
	sort.Strings(names)
	return names
}
