package procfs

// fd.go - what a process has open and where it is (W23-S03): fd/ (a link
// per descriptor to the path it was opened at, or pipe:[n], socket:[n],
// anon_inode:[...]), fdinfo/ (position and flags), the exe, cwd and root
// links, maps and mounts. The directories are 0500 and the links readable
// only by who may trace the process (its user, or root), as on Linux.
// Docs: WAVE-23-proc-virtio.md.

import (
	"fmt"
	"io/fs"
	"strconv"
	"strings"
	"syscall"

	"burrow/kernel/fsattr"
	"burrow/kernel/vfs"
)

// FD is one open descriptor.
type FD struct {
	Num    int
	Target string // what fd/N reads as
	Pos    int64
	Flags  int // O_ACCMODE and the status flags
}

// Map is one line of maps: an address range and what is there.
type Map struct {
	Start, End, Offset uint64
	Perms              string // "r-xp"
	Name               string // a path, [heap], [stack], or ""
}

// Mount is one line of mounts.
type Mount struct {
	Device, Point, Kind string
	ReadOnly            bool
}

const inoFD = 1 << 61 // fd and fdinfo inodes: pid, number and kind above this

// guarded is a link only a process allowed to trace pid may read.
func (f *FS) guarded(pid int, target string) func(vfs.Caller) (string, error) {
	return func(c vfs.Caller) (string, error) {
		if c.PID != 0 && !f.src.MayTrace(c, pid) {
			return "", syscall.EACCES
		}
		return target, nil
	}
}

func (f *FS) link(name string, a fsattr.Attr, mode fs.FileMode, target func(vfs.Caller) (string, error)) node {
	a.Nlink = 1
	return node{name: name, mode: fs.ModeSymlink | mode, attr: a, link: target}
}

// procLink is exe, cwd or root.
func (f *FS) procLink(p *Proc, a fsattr.Attr, name string) (node, bool) {
	cwd := p.Cwd
	if cwd == "" { // the kernel's init
		cwd = "/"
	}
	targets := map[string]string{"exe": p.Exe, "cwd": cwd, "root": "/"}
	t, ok := targets[name]
	if !ok || (name == "exe" && t == "") {
		return node{}, false
	}
	a.Ino += 64 + uint64(len(name))
	return f.link(name, a, 0o777, f.guarded(p.PID, t)), true
}

// fdNode is fd/ or fdinfo/ (kind), or one entry in it.
func (f *FS) fdNode(p *Proc, a fsattr.Attr, kind string, rest []string) (node, error) {
	if len(rest) == 0 {
		a.Ino = inoFD | uint64(p.PID)<<24 | map[string]uint64{"fd": 1, "fdinfo": 2}[kind]
		d := f.dir(kind, a, func() []string {
			names := make([]string, len(p.Files))
			for i, fd := range p.Files {
				names[i] = strconv.Itoa(fd.Num)
			}
			return names
		})
		d.mode = fs.ModeDir | 0o500
		return d, nil
	}
	for _, fd := range p.Files {
		if len(rest) != 1 || strconv.Itoa(fd.Num) != rest[0] {
			continue
		}
		a.Ino = inoFD | uint64(p.PID)<<24 | uint64(fd.Num)<<4 | 3
		if kind == "fdinfo" {
			info := fmt.Sprintf("pos:\t%d\nflags:\t0%o\nmnt_id:\t0\n", fd.Pos, fd.Flags)
			return f.file(rest[0], fileMode, a, func() []byte { return []byte(info) }), nil
		}
		mode := fs.FileMode(0o500)
		if fd.Flags&3 != 0 { // O_WRONLY or O_RDWR
			mode = 0o700
		}
		return f.link(rest[0], a, mode, f.guarded(p.PID, fd.Target)), nil
	}
	return node{}, fs.ErrNotExist
}

// maps is /proc/<pid>/maps: "start-end perms offset dev inode name".
func maps(p *Proc) string {
	var b strings.Builder
	for _, m := range p.Maps {
		line := fmt.Sprintf("%08x-%08x %s %08x 00:00 0", m.Start, m.End, m.Perms, m.Offset)
		if m.Name != "" {
			line = fmt.Sprintf("%-72s %s", line, m.Name)
		}
		b.WriteString(line + "\n")
	}
	return b.String()
}

// mounts is /proc/<pid>/mounts: "device point kind options 0 0".
func mounts(ms []Mount) string {
	var b strings.Builder
	for _, m := range ms {
		opt := "rw"
		if m.ReadOnly {
			opt = "ro"
		}
		fmt.Fprintf(&b, "%s %s %s %s 0 0\n", m.Device, m.Point, m.Kind, opt)
	}
	return b.String()
}
