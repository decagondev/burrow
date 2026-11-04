package proc

// metacalls.go - names and metadata (W13-S05): rename, renameat and
// renameat2 (RENAME_NOREPLACE), handed to the VFS (inside one mount, as
// Linux); fsync and fdatasync, which have nothing to wait for (a write is
// on the disk when write returns: wfile writes the whole file back and FAT
// flushes each change); and utimensat, accepted when the file exists and
// changing nothing. Permissions are checked as perm.go says (W19-S04); the
// owner calls are ownercalls.go. Docs: POSIX_COVERAGE.md.

import (
	"io/fs"
	"syscall"
)

const renameNoreplace = 1

type renamer interface {
	Rename(oldname, newname string) error
}

func (p *Process) renameAt(olddirfd, oldVA, newdirfd, newVA, flags uint64) int64 {
	if flags&^renameNoreplace != 0 {
		return -int64(syscall.EINVAL)
	}
	from, err := p.at(olddirfd, oldVA)
	if err != nil {
		return errno(err)
	}
	to, err := p.at(newdirfd, newVA)
	if err != nil {
		return errno(err)
	}
	c := p.Cred()
	if err := p.search(&c, from); err != nil {
		return errno(err)
	}
	src, err := fs.Stat(p.sys.files, from)
	if err != nil {
		return errno(err)
	}
	dst, err := fs.Stat(p.sys.files, to)
	if err == nil && flags&renameNoreplace != 0 {
		return -int64(syscall.EEXIST)
	}
	if err := p.mayRemove(&c, from, src); err != nil { // W19-S04: both directories, the sticky rule on both names
		return errno(err)
	}
	if err := p.mayCreate(&c, to); err != nil {
		return errno(err)
	}
	if dst != nil {
		if err := p.mayRemove(&c, to, dst); err != nil {
			return errno(err)
		}
	}
	r, ok := p.sys.files.(renamer)
	if !ok {
		return -int64(syscall.EROFS)
	}
	return errno(r.Rename(from, to))
}

const atFDCWDArg = uint64(atFDCWD & 0xFFFFFFFF)

func sysRenameFn(p *Process, a [6]uint64) (int64, bool) {
	return ok(p.renameAt(atFDCWDArg, a[0], atFDCWDArg, a[1], 0))
}

func sysRenameatFn(p *Process, a [6]uint64) (int64, bool) {
	return ok(p.renameAt(a[0], a[1], a[2], a[3], 0))
}

func sysRenameat2Fn(p *Process, a [6]uint64) (int64, bool) {
	return ok(p.renameAt(a[0], a[1], a[2], a[3], a[4]))
}

// fsync, fdatasync: the descriptor must be open; the data is already down.
func sysFsyncFn(p *Process, a [6]uint64) (int64, bool) {
	if p.fds.get(a[0]) == nil {
		return ok(-int64(syscall.EBADF))
	}
	return ok(0)
}

// pathExists answers the metadata calls that take a path: 0 if it exists.
func (p *Process) pathExists(dirfd, nameVA uint64) int64 {
	name, err := p.at(dirfd, nameVA)
	if err != nil {
		return errno(err)
	}
	c := p.Cred() // W19-S04: search permission on the way
	if err := p.search(&c, name); err != nil {
		return errno(err)
	}
	if _, err := fs.Stat(p.sys.files, name); err != nil {
		return errno(err)
	}
	return 0
}

// utimensat(dirfd, path, times, flags): a NULL path means dirfd itself.
func sysUtimensatFn(p *Process, a [6]uint64) (int64, bool) {
	if a[1] == 0 {
		return sysFsyncFn(p, [6]uint64{a[0]})
	}
	return ok(p.pathExists(a[0], a[1]))
}

func init() {
	for nr, h := range map[uint64]handler{
		82: sysRenameFn, 264: sysRenameatFn, 316: sysRenameat2Fn,
		74: sysFsyncFn, 75: sysFsyncFn, // fsync, fdatasync (chmod and chown: ownercalls.go)
		280: sysUtimensatFn,
	} {
		table[nr] = h
	}
}
