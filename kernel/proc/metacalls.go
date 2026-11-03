package proc

// metacalls.go - names and metadata (W13-S05): rename, renameat and
// renameat2 (RENAME_NOREPLACE), handed to the VFS (inside one mount, as
// Linux); fsync and fdatasync, which have nothing to wait for (a write is
// on the disk when write returns: wfile writes the whole file back and FAT
// flushes each change); and the permission, owner and time calls, which
// FAT and memfs have no place for: accepted when the file exists, changing
// nothing. Docs: POSIX_COVERAGE.md.

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
	if _, err := fs.Stat(p.sys.files, from); err != nil {
		return errno(err)
	}
	if _, err := fs.Stat(p.sys.files, to); err == nil && flags&renameNoreplace != 0 {
		return -int64(syscall.EEXIST)
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
	if _, err := fs.Stat(p.sys.files, name); err != nil {
		return errno(err)
	}
	return 0
}

func sysChmodFn(p *Process, a [6]uint64) (int64, bool) { // chmod, chown, lchown (path first)
	return ok(p.pathExists(atFDCWDArg, a[0]))
}

func sysFchmodatFn(p *Process, a [6]uint64) (int64, bool) { // fchmodat, fchownat (dirfd, path)
	return ok(p.pathExists(a[0], a[1]))
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
		74: sysFsyncFn, 75: sysFsyncFn, 91: sysFsyncFn, 93: sysFsyncFn, // fsync, fdatasync, fchmod, fchown
		90: sysChmodFn, 92: sysChmodFn, 94: sysChmodFn, // chmod, chown, lchown
		268: sysFchmodatFn, 260: sysFchmodatFn, // fchmodat, fchownat
		280: sysUtimensatFn,
	} {
		table[nr] = h
	}
}
