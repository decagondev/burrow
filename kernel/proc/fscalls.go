package proc

// fscalls.go - what programs ask about files beyond stat (W16-S02, from
// the X census: fontconfig asks both): posix_fadvise (fadvise64), which has
// nothing to tune and is accepted for any open file, and statfs/fstatfs,
// which name the file system holding a path with Linux's magic number. The
// sizes are not tracked (blocks and inodes read as 0), as for /proc on
// Linux. link and linkat answer EPERM for a source that exists: no Burrow
// file system has hard links, and EPERM is what Linux's vfat says, which
// sends fontconfig's cache lock to its mkdir fallback. Docs: docs/02-architecture/POSIX_COVERAGE.md.

import (
	"encoding/binary"
	"syscall"
)

const (
	statfsBytes = 120 // struct statfs on x86-64
	nameMax     = 255
)

// fsMagic is f_type for each kind of mount (vfs.VFS.KindOf).
var fsMagic = map[string]uint64{
	"tarfs": 0x858458f6, // RAMFS_MAGIC: the initrd lives in memory
	"ramfs": 0x01021994, // TMPFS_MAGIC
	"devfs": 0x01021994, // devtmpfs reports TMPFS_MAGIC
	"fat32": 0x4d44,     // MSDOS_SUPER_MAGIC
}

type kindFS interface{ KindOf(name string) string }

// statfs is struct statfs for the file system holding name.
func (p *Process) statfs(name string) []byte {
	kind := ""
	if k, ok := p.sys.files.(kindFS); ok {
		kind = k.KindOf(name)
	}
	b := make([]byte, statfsBytes)
	le := binary.LittleEndian
	le.PutUint64(b[0:], fsMagic[kind])
	le.PutUint64(b[8:], PageSize)  // f_bsize
	le.PutUint64(b[64:], nameMax)  // f_namelen
	le.PutUint64(b[72:], PageSize) // f_frsize
	return b
}

func sysStatfsFn(p *Process, a [6]uint64) (int64, bool) {
	name, err := p.at(atFDCWDArg, a[0])
	if err != nil {
		return ok(errno(err))
	}
	if e := p.pathExists(atFDCWDArg, a[0]); e < 0 {
		return ok(e)
	}
	return ok(errno(p.mem.CopyOut(a[1], p.statfs(name))))
}

func sysFstatfsFn(p *Process, a [6]uint64) (int64, bool) {
	f := p.fds.get(a[0])
	if f == nil {
		return ok(-int64(syscall.EBADF))
	}
	return ok(errno(p.mem.CopyOut(a[1], p.statfs(f.path))))
}

// fadvise64(fd, offset, len, advice): nothing to tune; pipes and sockets
// are ESPIPE, as on Linux.
func sysFadviseFn(p *Process, a [6]uint64) (int64, bool) {
	f := p.fds.get(a[0])
	switch {
	case f == nil:
		return ok(-int64(syscall.EBADF))
	case a[3] > 5: // POSIX_FADV_NORMAL..NOREUSE
		return ok(-int64(syscall.EINVAL))
	}
	switch f.f.(type) {
	case *pipeEnd, *sockEnd:
		return ok(-int64(syscall.ESPIPE))
	}
	return ok(0)
}

// link(old, new) and linkat(olddirfd, old, newdirfd, new, flags).
func sysLinkFn(p *Process, a [6]uint64) (int64, bool) { return ok(p.linkAt(atFDCWDArg, a[0])) }

func sysLinkatFn(p *Process, a [6]uint64) (int64, bool) { return ok(p.linkAt(a[0], a[1])) }

func (p *Process) linkAt(dirfd, oldVA uint64) int64 {
	if e := p.pathExists(dirfd, oldVA); e < 0 {
		return e
	}
	return -int64(syscall.EPERM)
}

func init() {
	for nr, h := range map[uint64]handler{137: sysStatfsFn, 138: sysFstatfsFn, 221: sysFadviseFn,
		86: sysLinkFn, 265: sysLinkatFn} {
		table[nr] = h
	}
}
