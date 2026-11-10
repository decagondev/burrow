package proc

// ownercalls.go - chmod and chown (W19-S04): chmod, fchmod, fchmodat,
// chown, fchown, lchown and fchownat, with Linux's rules: only the owner
// (or root) changes a mode, and a user's chmod of a file whose group is not
// one of theirs drops its set-group-id bit; only root gives a file away; an
// owner may change its group to one they are in; a chown of anything but a
// directory drops the set-user-id bit, and the set-group-id bit when group
// execute is set, even root's. A file system that keeps no owners says
// EPERM (vfs). Descriptors without a path (pipes, sockets) accept and
// change nothing. lchown (and fchownat's AT_SYMLINK_NOFOLLOW) changes a
// link's own owner (W19-S05). A pseudo-terminal's slave keeps its owner and
// mode beside the pair (ptyowner.go, W24-S03).
// Docs: POSIX_COVERAGE.md.

import (
	"io/fs"
	"syscall"

	"burrow/kernel/fsattr"
	"burrow/kernel/vfs"
)

// ownerFS is the VFS's chmod and chown.
type ownerFS interface {
	Chmod(name string, mode fs.FileMode) error
	Chown(name string, uid, gid uint32) error
}

func (p *Process) owners() (ownerFS, bool) {
	o, ok := p.sys.files.(ownerFS)
	return o, ok
}

// target is the file a chmod/chown call names: by path from dirfd, or the
// descriptor itself (fchmod, fchown, AT_EMPTY_PATH); "" with no error is a
// descriptor without a path.
func (p *Process) target(dirfd, nameVA uint64, byFD bool) (string, error) {
	return p.targetFollow(dirfd, nameVA, byFD, true)
}

func (p *Process) targetFollow(dirfd, nameVA uint64, byFD, follow bool) (string, error) {
	if byFD {
		f := p.fds.get(dirfd)
		if f == nil {
			return "", syscall.EBADF
		}
		return f.path, nil
	}
	return p.atFollow(dirfd, nameVA, follow)
}

func (p *Process) chmodAt(name string, bits uint32) int64 {
	if name == "" {
		return 0
	}
	c := p.Cred()
	info, err := p.statPath(name, true)
	if err != nil {
		return errno(err)
	}
	if !c.owns(info) {
		return -int64(syscall.EPERM)
	}
	mode := unixToMode(bits)
	if a, _ := vfs.AttrOf(info); !c.root() && !c.inGroup(a.GID) {
		mode &^= fs.ModeSetgid
	}
	if ptyNumber(name) >= 0 { // a pseudo-terminal's slave (W24-S03, ptyowner.go)
		return errno(p.sys.setPTY(name, 0, 0, mode, true))
	}
	o, ok := p.owners()
	if !ok {
		return -int64(syscall.EROFS)
	}
	return errno(o.Chmod(name, mode))
}

func (p *Process) chownAt(name string, uid, gid uint32, follow bool) int64 {
	if name == "" {
		return 0
	}
	c := p.Cred()
	info, err := p.statPath(name, follow)
	if err != nil {
		return errno(err)
	}
	a, _ := vfs.AttrOf(info)
	if !c.root() && (!c.owns(info) || uid != fsattr.Keep && uid != a.UID || gid != fsattr.Keep && gid != a.GID && !c.inGroup(gid)) {
		return -int64(syscall.EPERM)
	}
	if ptyNumber(name) >= 0 { // a pseudo-terminal's slave (W24-S03, ptyowner.go)
		return errno(p.sys.setPTY(name, uid, gid, 0, false))
	}
	o, ok := p.owners()
	if !ok {
		return -int64(syscall.EROFS)
	}
	if isLink(info) { // lchown (W19-S05): the link's own owner; no mode to fix
		l, ok := p.sys.files.(interface {
			Lchown(string, uint32, uint32) error
		})
		if !ok {
			return -int64(syscall.EPERM)
		}
		return errno(l.Lchown(name, uid, gid))
	}
	if err := o.Chown(name, uid, gid); err != nil {
		return errno(err)
	}
	m := info.Mode()
	if drop := m & (fs.ModeSetuid | fs.ModeSetgid); !m.IsDir() && drop != 0 {
		if m&0o010 == 0 {
			drop &^= fs.ModeSetgid // without group execute it marks locking: kept
		}
		return errno(o.Chmod(name, m&(fs.ModePerm|fsattr.SpecialBits)&^drop))
	}
	return 0
}

func init() {
	for nr, h := range map[uint64]handler{
		90: func(p *Process, a [6]uint64) (int64, bool) { // chmod(path, mode)
			name, err := p.target(atFDCWDArg, a[0], false)
			return ok(orErr(err, func() int64 { return p.chmodAt(name, uint32(a[1])) }))
		},
		91: func(p *Process, a [6]uint64) (int64, bool) { // fchmod(fd, mode)
			name, err := p.target(a[0], 0, true)
			return ok(orErr(err, func() int64 { return p.chmodAt(name, uint32(a[1])) }))
		},
		268: func(p *Process, a [6]uint64) (int64, bool) { // fchmodat(dirfd, path, mode)
			name, err := p.target(a[0], a[1], false)
			return ok(orErr(err, func() int64 { return p.chmodAt(name, uint32(a[2])) }))
		},
		92: chownFn(true), 94: chownFn(false), // chown, lchown(path, uid, gid)
		93: func(p *Process, a [6]uint64) (int64, bool) { // fchown(fd, uid, gid)
			name, err := p.target(a[0], 0, true)
			return ok(orErr(err, func() int64 { return p.chownAt(name, uint32(a[1]), uint32(a[2]), true) }))
		},
		260: func(p *Process, a [6]uint64) (int64, bool) { // fchownat(dirfd, path, uid, gid, flags)
			name, _ := p.mem.CString(a[1], 4096)
			follow := a[4]&atSymlinkNofollw == 0
			name, err := p.targetFollow(a[0], a[1], name == "" && a[4]&atEmptyPath != 0, follow)
			return ok(orErr(err, func() int64 { return p.chownAt(name, uint32(a[2]), uint32(a[3]), follow) }))
		},
	} {
		table[nr] = h
	}
}

// chownFn is chown (follow) or lchown (not).
func chownFn(follow bool) handler {
	return func(p *Process, a [6]uint64) (int64, bool) {
		name, err := p.targetFollow(atFDCWDArg, a[0], false, follow)
		return ok(orErr(err, func() int64 { return p.chownAt(name, uint32(a[1]), uint32(a[2]), follow) }))
	}
}

// orErr is -errno for err, else what fn returns.
func orErr(err error, fn func() int64) int64 {
	if err != nil {
		return errno(err)
	}
	return fn()
}
