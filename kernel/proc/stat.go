package proc

// stat.go - the stat family: stat, lstat, fstat, newfstatat, and statx
// (W14-S03, from the same struct stat). There are no symbolic links yet, so
// lstat is stat and readlink says "not a link" (-EINVAL). The inode number,
// links, owner and times come from the file system's vfs.Attr when it gives
// one (ext2 since W19-S01); otherwise the owner is root and the inode a hash
// of the path. Never decides anything from them. Docs: POSIX_COVERAGE.md.

import (
	"encoding/binary"
	"hash/fnv"
	"io/fs"
	"syscall"

	"burrow/kernel/vfs"
)

const (
	statBytes  = 144 // struct stat
	statxBytes = 256 // struct statx

	statxBasicStats = 0x7ff      // STATX_TYPE ... STATX_BLOCKS
	statxReserved   = 0x80000000 // STATX__RESERVED
	atNoAutomount   = 0x800
	atStatxSyncType = 0x6000
)

// statBuf is Linux's struct stat for info.
func statBuf(name string, info fs.FileInfo) []byte {
	st := make([]byte, statBytes)
	put := func(off int, v uint64) { binary.LittleEndian.PutUint64(st[off:], v) }
	h := fnv.New64a()
	h.Write([]byte(name))
	mtime := uint64(info.ModTime().Unix())
	ino, nlink, atime, ctime := h.Sum64()|1, uint64(1), mtime, mtime // st_ino: stable per path
	if a, ok := vfs.AttrOf(info); ok {
		ino, nlink = a.Ino, uint64(a.Nlink)
		atime, ctime = uint64(a.Atime.Unix()), uint64(a.Ctime.Unix())
		binary.LittleEndian.PutUint32(st[28:], a.UID) // st_uid
		binary.LittleEndian.PutUint32(st[32:], a.GID) // st_gid
	}
	put(8, ino)
	put(16, nlink)
	binary.LittleEndian.PutUint32(st[24:], uint32(unixMode(info.Mode()))) // st_mode
	put(48, uint64(info.Size()))
	put(56, PageSize)                    // st_blksize
	put(64, uint64(info.Size()+511)/512) // st_blocks
	put(88, mtime)                       // st_mtim
	put(72, atime)                       // st_atim
	put(104, ctime)                      // st_ctim
	return st
}

func unixMode(m fs.FileMode) uint64 {
	t := uint64(syscall.S_IFREG)
	switch {
	case m.IsDir():
		t = syscall.S_IFDIR
	case m&fs.ModeCharDevice != 0:
		t = syscall.S_IFCHR
	case m&fs.ModeDevice != 0:
		t = syscall.S_IFBLK
	case m&fs.ModeSymlink != 0:
		t = syscall.S_IFLNK
	case m&fs.ModeNamedPipe != 0:
		t = syscall.S_IFIFO
	case m&fs.ModeSocket != 0:
		t = syscall.S_IFSOCK
	}
	if m&fs.ModeSetuid != 0 {
		t |= syscall.S_ISUID
	}
	if m&fs.ModeSetgid != 0 {
		t |= syscall.S_ISGID
	}
	if m&fs.ModeSticky != 0 {
		t |= syscall.S_ISVTX
	}
	return t | uint64(m.Perm())
}

// fdStat is struct stat for an open descriptor, or -errno.
func (p *Process) fdStat(fd uint64) ([]byte, int64) {
	f := p.fds.get(fd)
	if f == nil {
		return nil, -int64(syscall.EBADF)
	}
	if f.console != nil {
		return statBuf("dev/console", consoleInfo{}), 0
	}
	info, err := f.f.Stat()
	if err != nil {
		return nil, errno(err)
	}
	return statBuf(f.path, info), 0
}

// statOf is struct stat for name relative to dirfd; with AT_EMPTY_PATH and
// an empty (or NULL) name, for dirfd itself.
func (p *Process) statOf(dirfd, nameVA, flags uint64) ([]byte, int64) {
	if flags&atEmptyPath != 0 {
		if nameVA == 0 {
			return p.fdStat(dirfd)
		}
		if s, err := p.mem.CString(nameVA, 1); err == nil && s == "" {
			return p.fdStat(dirfd)
		}
	}
	name, err := p.at(dirfd, nameVA)
	if err != nil {
		return nil, errno(err)
	}
	info, err := fs.Stat(p.sys.files, name)
	if err != nil {
		return nil, errno(err)
	}
	return statBuf(name, info), 0
}

func (p *Process) copyStat(st []byte, e int64, bufVA uint64) int64 {
	if e < 0 {
		return e
	}
	return errno(p.mem.CopyOut(bufVA, st))
}

func sysFstatFn(p *Process, a [6]uint64) (int64, bool) {
	st, e := p.fdStat(a[0])
	return ok(p.copyStat(st, e, a[1]))
}

// stat and lstat (no symbolic links yet: the same thing).
func sysStatFn(p *Process, a [6]uint64) (int64, bool) {
	st, e := p.statOf(atFDCWDArg, a[0], 0)
	return ok(p.copyStat(st, e, a[1]))
}

func sysNewfstatatFn(p *Process, a [6]uint64) (int64, bool) {
	st, e := p.statOf(a[0], a[1], a[3])
	return ok(p.copyStat(st, e, a[2]))
}

// statx(dirfd, path, flags, mask, buf): every basic field, whatever mask
// asks for (Linux may return more than asked); no birth time.
func sysStatxFn(p *Process, a [6]uint64) (int64, bool) {
	if a[2]&^(atEmptyPath|atSymlinkNofollw|atNoAutomount|atStatxSyncType) != 0 || a[3]&statxReserved != 0 {
		return ok(-int64(syscall.EINVAL))
	}
	st, e := p.statOf(a[0], a[1], a[2])
	if e < 0 {
		return ok(e)
	}
	return ok(errno(p.mem.CopyOut(a[4], statx(st))))
}

// statx converts a struct stat into a struct statx.
func statx(st []byte) []byte {
	le := binary.LittleEndian
	x := make([]byte, statxBytes)
	le.PutUint32(x[0:], statxBasicStats)
	le.PutUint32(x[4:], uint32(le.Uint64(st[56:])))              // blksize
	le.PutUint32(x[16:], uint32(le.Uint64(st[16:])))             // nlink
	le.PutUint32(x[20:], le.Uint32(st[28:]))                     // uid
	le.PutUint32(x[24:], le.Uint32(st[32:]))                     // gid
	le.PutUint16(x[28:], uint16(le.Uint32(st[24:])))             // mode
	le.PutUint64(x[32:], le.Uint64(st[8:]))                      // ino
	le.PutUint64(x[40:], le.Uint64(st[48:]))                     // size
	le.PutUint64(x[48:], le.Uint64(st[64:]))                     // blocks
	for _, t := range [][2]int{{64, 72}, {96, 104}, {112, 88}} { // atime, ctime, mtime
		le.PutUint64(x[t[0]:], le.Uint64(st[t[1]:]))
		le.PutUint32(x[t[0]+8:], uint32(le.Uint64(st[t[1]+8:])))
	}
	return x
}

// readlink and readlinkat: nothing is a symbolic link, so a path that
// exists is -EINVAL, as Linux answers for a regular file.
func sysReadlinkFn(p *Process, a [6]uint64) (int64, bool) {
	return ok(p.readlinkAt(atFDCWDArg, a[0], a[2]))
}

func sysReadlinkatFn(p *Process, a [6]uint64) (int64, bool) {
	return ok(p.readlinkAt(a[0], a[1], a[3]))
}

func (p *Process) readlinkAt(dirfd, nameVA, size uint64) int64 {
	if int64(size) <= 0 {
		return -int64(syscall.EINVAL)
	}
	if e := p.pathExists(dirfd, nameVA); e < 0 {
		return e
	}
	return -int64(syscall.EINVAL)
}

func init() {
	for nr, h := range map[uint64]handler{
		332: sysStatxFn, 89: sysReadlinkFn, 267: sysReadlinkatFn,
	} {
		table[nr] = h
	}
}
