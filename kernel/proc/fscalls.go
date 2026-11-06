package proc

// fscalls.go - what programs ask about files beyond stat (W16-S02, from
// the X census: fontconfig asks both): posix_fadvise (fadvise64), which has
// nothing to tune and is accepted for any open file, and statfs/fstatfs,
// which name the file system holding a path with Linux's magic number and,
// since W23-S03, its size in blocks when the file system knows it (FAT,
// ext2, the initrd; a writable ramfs is sized as Linux sizes tmpfs, half
// the RAM); devfs and /proc read as 0, as /proc does on Linux. link and linkat moved to linkcalls.go when Burrow got hard links
// (W19-S05); on FAT they still say EPERM, as vfat does, which sends
// fontconfig's cache lock to its mkdir fallback there.
// Docs: docs/02-architecture/POSIX_COVERAGE.md.

import (
	"encoding/binary"
	"syscall"

	"burrow/kernel/fsattr"
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
	"ext2":  0xef53,     // EXT2_SUPER_MAGIC
	"proc":  0x9fa0,     // PROC_SUPER_MAGIC
}

type spaceFS interface {
	Space(name string) (fsattr.Space, bool)
}

// space is the size of the file system holding name, a ramfs sized as tmpfs.
func (p *Process) space(name string) fsattr.Space {
	sp, ok := p.sys.files.(spaceFS)
	if !ok {
		return fsattr.Space{}
	}
	s, _ := sp.Space(name)
	if s.Total == 0 && p.kindOf(name) == "ramfs" {
		total, _ := p.sys.nu.MemInfo()
		s.Total = total / 2
		s.Free = s.Total - min(s.Used, s.Total)
	}
	return s
}

func (p *Process) kindOf(name string) string {
	if k, ok := p.sys.files.(kindFS); ok {
		return k.KindOf(name)
	}
	return ""
}

type kindFS interface{ KindOf(name string) string }

// statfs is struct statfs for the file system holding name.
func (p *Process) statfs(name string) []byte {
	b := make([]byte, statfsBytes)
	le := binary.LittleEndian
	le.PutUint64(b[0:], fsMagic[p.kindOf(name)])
	s := p.space(name)
	le.PutUint64(b[16:], s.Total/PageSize) // f_blocks
	le.PutUint64(b[24:], s.Free/PageSize)  // f_bfree
	le.PutUint64(b[32:], s.Free/PageSize)  // f_bavail
	le.PutUint64(b[8:], PageSize)          // f_bsize
	le.PutUint64(b[64:], nameMax)          // f_namelen
	le.PutUint64(b[72:], PageSize)         // f_frsize
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

func init() {
	for nr, h := range map[uint64]handler{137: sysStatfsFn, 138: sysFstatfsFn, 221: sysFadviseFn} { // links: linkcalls.go
		table[nr] = h
	}
}
