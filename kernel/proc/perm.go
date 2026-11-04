package proc

// perm.go - permission checks (W19-S04), as Linux makes them: a file's
// owner class is chosen once (owner, else group, else others) and only its
// bits count; every directory on a path needs search (execute) permission;
// creating or removing a name needs write and search on its directory, and
// in a sticky directory removing one needs owning the file or the
// directory. Root (fs uid 0) passes every check except running a file with
// no execute bit at all. The checks use the fs ids; access(2) uses the real
// ones. A file system that keeps no owners (FAT, devfs) has root own every
// file. Never changes anything. Docs: docs/04-plan/waves/WAVE-19-unix.md.

import (
	"io/fs"
	"path"
	"strings"
	"syscall"
	"time"

	"burrow/kernel/vfs"
)

const (
	mayExec  = 1
	mayWrite = 2
	mayRead  = 4
)

func (c *Cred) inGroup(gid uint32) bool {
	if gid == c.GID[idFS] {
		return true
	}
	for _, g := range c.Groups {
		if g == gid {
			return true
		}
	}
	return false
}

func (c *Cred) root() bool { return c.UID[idFS] == 0 }

// owns says whether c owns the file, or is root (chmod, utimes, sticky).
func (c *Cred) owns(info fs.FileInfo) bool {
	a, _ := vfs.AttrOf(info)
	return c.root() || c.UID[idFS] == a.UID
}

// allows says whether c may do want (mayRead|mayWrite|mayExec) to the file.
func (c *Cred) allows(info fs.FileInfo, want uint32) bool {
	perm := uint32(info.Mode().Perm())
	if c.root() {
		return want&mayExec == 0 || info.IsDir() || perm&0o111 != 0
	}
	a, _ := vfs.AttrOf(info)
	bits := perm // others
	switch {
	case c.UID[idFS] == a.UID:
		bits = perm >> 6
	case c.inGroup(a.GID):
		bits = perm >> 3
	}
	return bits&want == want
}

// search checks that c may pass through every directory above name (an
// io/fs path), the root included: ENOENT, ENOTDIR or EACCES on the first
// that fails, in path order.
func (p *Process) search(c *Cred, name string) error {
	if name == "." || c.root() {
		return nil
	}
	dir := "."
	parts := strings.Split(name, "/")
	for i := 0; i < len(parts); i++ {
		info, err := fs.Stat(p.sys.files, dir)
		switch {
		case err != nil:
			return err
		case !info.IsDir():
			return syscall.ENOTDIR
		case !c.allows(info, mayExec):
			return syscall.EACCES
		}
		if i == len(parts)-1 {
			break
		}
		dir = path.Join(dir, parts[i])
	}
	return nil
}

// permit checks the path to name, then want on name itself.
func (p *Process) permit(c *Cred, name string, want uint32) error {
	if err := p.search(c, name); err != nil {
		return err
	}
	info, err := fs.Stat(p.sys.files, name)
	if err != nil {
		return err
	}
	if !c.allows(info, want) {
		return syscall.EACCES
	}
	return nil
}

// mayCreate checks that c may add (or take away) a name in name's directory.
func (p *Process) mayCreate(c *Cred, name string) error {
	return p.permit(c, path.Dir(name), mayWrite|mayExec)
}

// mayRemove is mayCreate plus the sticky bit's rule, for victim (name's file).
func (p *Process) mayRemove(c *Cred, name string, victim fs.FileInfo) error {
	if err := p.mayCreate(c, name); err != nil {
		return err
	}
	dir, err := fs.Stat(p.sys.files, path.Dir(name))
	if err != nil {
		return err
	}
	if dir.Mode()&fs.ModeSticky != 0 && !c.owns(victim) && !c.owns(dir) {
		return syscall.EPERM
	}
	return nil
}

// newAttrs is what c creates name with: c's fs ids, mode less the umask,
// and the directory's group (and, for a directory, its set-group-id bit)
// when the directory is set-group-id.
func (p *Process) newAttrs(c *Cred, name string, mode fs.FileMode, isDir bool) vfs.NewFile {
	p.mu.Lock()
	umask := fs.FileMode(p.umask)
	p.mu.Unlock()
	n := vfs.NewFile{UID: c.UID[idFS], GID: c.GID[idFS], Mode: mode &^ umask, Mtime: time.Now()}
	if dir, err := fs.Stat(p.sys.files, path.Dir(name)); err == nil && dir.Mode()&fs.ModeSetgid != 0 {
		a, _ := vfs.AttrOf(dir)
		n.GID = a.GID
		if isDir {
			n.Mode |= fs.ModeSetgid
		}
	}
	return n
}

// unixToMode is the permission, set-id and sticky bits of a mode_t.
func unixToMode(bits uint32) fs.FileMode {
	m := fs.FileMode(bits & 0o777)
	if bits&syscall.S_ISUID != 0 {
		m |= fs.ModeSetuid
	}
	if bits&syscall.S_ISGID != 0 {
		m |= fs.ModeSetgid
	}
	if bits&syscall.S_ISVTX != 0 {
		m |= fs.ModeSticky
	}
	return m
}

// accessWant is what open's flags ask of a file.
func accessWant(flags int) uint32 {
	want := uint32(0)
	switch flags & oAccMode {
	case oRDONLY:
		want = mayRead
	case oWRONLY:
		want = mayWrite
	default:
		want = mayRead | mayWrite
	}
	if flags&oTrunc != 0 {
		want |= mayWrite
	}
	return want
}
