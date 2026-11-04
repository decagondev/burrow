package proc

// linkcalls.go - making links (W19-S05): symlink and symlinkat (the
// target is stored as given, unchecked, as Linux does; the link's name
// must not exist), link and linkat (a second name for a file inside one
// mount: EXDEV across mounts, EPERM for a directory, the old name
// followed only with AT_SYMLINK_FOLLOW). Creating either needs write and
// search on the directory; a user may hard-link only a file they own or
// may read and write (Linux's protected_hardlinks). File systems without
// links (FAT, devfs) say EPERM. Docs: POSIX_COVERAGE.md.

import (
	"syscall"

	"burrow/kernel/vfs"
)

const atSymlinkFollow = 0x400

// linkFS is the VFS's link side.
type linkFS interface {
	Symlink(target, name string, n vfs.NewFile) error
	Link(oldname, newname string) error
}

// symlinkat(target, newdirfd, linkpath)
func (p *Process) symlinkAt(targetVA, dirfd, nameVA uint64) int64 {
	target, err := p.mem.CString(targetVA, 4096)
	if err != nil {
		return errno(err)
	}
	if target == "" {
		return -int64(syscall.ENOENT)
	}
	name, err := p.atNoFollow(dirfd, nameVA)
	if err != nil {
		return errno(err)
	}
	if _, err := p.statPath(name, false); err == nil {
		return -int64(syscall.EEXIST)
	}
	l, ok := p.sys.files.(linkFS)
	if !ok {
		return -int64(syscall.EPERM)
	}
	c := p.Cred()
	if err := p.mayCreate(&c, name); err != nil {
		return errno(err)
	}
	return errno(l.Symlink(target, name, p.newAttrs(&c, name, 0o777, false)))
}

// linkat(olddirfd, oldpath, newdirfd, newpath, flags)
func (p *Process) linkAt(olddirfd, oldVA, newdirfd, newVA, flags uint64) int64 {
	if flags&^atSymlinkFollow != 0 {
		return -int64(syscall.EINVAL)
	}
	from, err := p.atFollow(olddirfd, oldVA, flags&atSymlinkFollow != 0)
	if err != nil {
		return errno(err)
	}
	to, err := p.atNoFollow(newdirfd, newVA)
	if err != nil {
		return errno(err)
	}
	src, err := p.statPath(from, false)
	switch {
	case err != nil:
		return errno(err)
	case src.IsDir():
		return -int64(syscall.EPERM)
	}
	if _, err := p.statPath(to, false); err == nil {
		return -int64(syscall.EEXIST)
	}
	c := p.Cred()
	if !c.owns(src) && !c.allows(src, mayRead|mayWrite) {
		return -int64(syscall.EPERM) // protected_hardlinks
	}
	if err := p.mayCreate(&c, to); err != nil {
		return errno(err)
	}
	l, ok := p.sys.files.(linkFS)
	if !ok {
		return -int64(syscall.EPERM)
	}
	return errno(l.Link(from, to))
}

func init() {
	for nr, h := range map[uint64]handler{
		88: func(p *Process, a [6]uint64) (int64, bool) { // symlink(target, linkpath)
			return ok(p.symlinkAt(a[0], atFDCWDArg, a[1]))
		},
		266: func(p *Process, a [6]uint64) (int64, bool) { // symlinkat(target, newdirfd, linkpath)
			return ok(p.symlinkAt(a[0], a[1], a[2]))
		},
		86: func(p *Process, a [6]uint64) (int64, bool) { // link(old, new)
			return ok(p.linkAt(atFDCWDArg, a[0], atFDCWDArg, a[1], 0))
		},
		265: func(p *Process, a [6]uint64) (int64, bool) { // linkat(olddirfd, old, newdirfd, new, flags)
			return ok(p.linkAt(a[0], a[1], a[2], a[3], a[4]))
		},
	} {
		table[nr] = h
	}
}
