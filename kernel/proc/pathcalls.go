package proc

import (
	"encoding/binary"
	"errors"
	"hash/fnv"
	"io/fs"
	"path"
	"strings"
	"syscall"
)

// open(2) flags (Linux x86-64)
const (
	oRDONLY    = 0
	oWRONLY    = 1
	oRDWR      = 2
	oAccMode   = 3
	oCreat     = 0x40
	oExcl      = 0x80
	oTrunc     = 0x200
	oAppend    = 0x400
	oDirectory = 0x10000
	oCloexec   = 0x80000

	atFDCWD          = -100
	atRemoveDir      = 0x200
	atSymlinkNofollw = 0x100
	atEmptyPath      = 0x1000
)

// at resolves name for an *at call: absolute, from the cwd (AT_FDCWD), or
// from the directory open at dirfd.
func (p *Process) at(dirfd uint64, nameVA uint64) (string, error) {
	name, err := p.mem.CString(nameVA, 4096)
	if err != nil {
		return "", err
	}
	if strings.HasPrefix(name, "/") || int32(dirfd) == atFDCWD {
		return p.resolve(name), nil
	}
	d := p.fds.get(dirfd)
	if d == nil || d.f == nil || d.path == "" {
		return "", syscall.EBADF
	}
	return cleanPath(d.path + "/" + name), nil
}

func sysOpenFn(p *Process, a [6]uint64) (int64, bool) {
	return ok(p.openAt(uint64(atFDCWD&0xFFFFFFFF), a[0], int(a[1])))
}

func sysOpenatFn(p *Process, a [6]uint64) (int64, bool) { return ok(p.openAt(a[0], a[1], int(a[2]))) }

func (p *Process) openAt(dirfd, nameVA uint64, flags int) int64 {
	name, err := p.at(dirfd, nameVA)
	if err != nil {
		return errno(err)
	}
	fsys := p.sys.files
	info, statErr := fs.Stat(fsys, name)
	exists := statErr == nil
	var f fs.File
	switch {
	case !exists && flags&oCreat == 0:
		return errno(statErr)
	case exists && flags&oCreat != 0 && flags&oExcl != 0:
		return -int64(syscall.EEXIST)
	case exists && info.IsDir() && flags&oAccMode != oRDONLY:
		return -int64(syscall.EISDIR)
	case exists && !info.IsDir() && flags&oDirectory != 0:
		return -int64(syscall.ENOTDIR)
	case flags&oAccMode == oRDONLY && exists, exists && info.Mode()&fs.ModeDevice != 0:
		if f, err = fsys.Open(name); err != nil { // a device opened for writing writes to it
			return errno(err)
		}
	default:
		if f, err = p.writable(name, exists, flags); err != nil {
			return errno(err)
		}
	}
	d := newFile(f, nil)
	d.path, d.flags = name, flags&^(oCreat|oExcl|oTrunc|oCloexec)
	n, err := p.fds.add(d, 0, flags&oCloexec != 0)
	if err != nil {
		d.unref()
		return errno(err)
	}
	return int64(n)
}

// writable opens (or creates) a regular file for writing.
func (p *Process) writable(name string, exists bool, flags int) (fs.File, error) {
	wfs, isW := p.sys.files.(WritableFS)
	if !isW {
		return nil, syscall.EROFS
	}
	var data []byte
	if exists && flags&oTrunc == 0 {
		var err error
		if data, err = fs.ReadFile(wfs, name); err != nil {
			return nil, err
		}
	} else if err := wfs.WriteFile(name, nil); err != nil { // create or truncate
		return nil, err
	}
	return &wfile{fsys: wfs, name: name, data: data, append: flags&oAppend != 0}, nil
}

// statBuf is Linux's struct stat (144 bytes) for info.
func statBuf(name string, info fs.FileInfo) []byte {
	st := make([]byte, 144)
	put := func(off int, v uint64) { binary.LittleEndian.PutUint64(st[off:], v) }
	h := fnv.New64a()
	h.Write([]byte(name))
	put(8, h.Sum64()|1)                                                   // st_ino: stable per path
	put(16, 1)                                                            // st_nlink
	binary.LittleEndian.PutUint32(st[24:], uint32(unixMode(info.Mode()))) // st_mode
	put(48, uint64(info.Size()))
	put(56, PageSize)                       // st_blksize
	put(64, uint64(info.Size()+511)/512)    // st_blocks
	put(88, uint64(info.ModTime().Unix()))  // st_mtim
	put(72, uint64(info.ModTime().Unix()))  // st_atim
	put(104, uint64(info.ModTime().Unix())) // st_ctim
	return st
}

func unixMode(m fs.FileMode) uint64 {
	t := uint64(syscall.S_IFREG)
	switch {
	case m.IsDir():
		t = syscall.S_IFDIR
	case m&fs.ModeCharDevice != 0:
		t = syscall.S_IFCHR
	case m&fs.ModeNamedPipe != 0:
		t = syscall.S_IFIFO
	}
	return t | uint64(m.Perm())
}

func sysFstatFn(p *Process, a [6]uint64) (int64, bool) { return ok(p.fstat(a[0], a[1])) }

func (p *Process) fstat(fd, bufVA uint64) int64 {
	f := p.fds.get(fd)
	if f == nil {
		return -int64(syscall.EBADF)
	}
	if f.console != nil {
		return errno(p.mem.CopyOut(bufVA, statBuf("dev/console", consoleInfo{})))
	}
	info, err := f.f.Stat()
	if err != nil {
		return errno(err)
	}
	return errno(p.mem.CopyOut(bufVA, statBuf(f.path, info)))
}

// stat and lstat (no symbolic links yet: the same thing).
func sysStatFn(p *Process, a [6]uint64) (int64, bool) {
	return ok(p.statAt(uint64(atFDCWD&0xFFFFFFFF), a[0], a[1], 0))
}

func sysNewfstatatFn(p *Process, a [6]uint64) (int64, bool) {
	return ok(p.statAt(a[0], a[1], a[2], a[3]))
}

func (p *Process) statAt(dirfd, nameVA, bufVA, flags uint64) int64 {
	if flags&atEmptyPath != 0 {
		if s, err := p.mem.CString(nameVA, 1); err == nil && s == "" {
			return p.fstat(dirfd, bufVA)
		}
	}
	name, err := p.at(dirfd, nameVA)
	if err != nil {
		return errno(err)
	}
	info, err := fs.Stat(p.sys.files, name)
	if err != nil {
		return errno(err)
	}
	return errno(p.mem.CopyOut(bufVA, statBuf(name, info)))
}

// access/faccessat: existence only (everyone is root).
func sysAccessFn(p *Process, a [6]uint64) (int64, bool) {
	return ok(p.accessAt(uint64(atFDCWD&0xFFFFFFFF), a[0]))
}

func sysFaccessatFn(p *Process, a [6]uint64) (int64, bool) { return ok(p.accessAt(a[0], a[1])) }

func (p *Process) accessAt(dirfd, nameVA uint64) int64 {
	name, err := p.at(dirfd, nameVA)
	if err != nil {
		return errno(err)
	}
	_, err = fs.Stat(p.sys.files, name)
	return errno(err)
}

func sysGetcwdFn(p *Process, a [6]uint64) (int64, bool) {
	p.mu.Lock()
	cwd := p.cwd
	p.mu.Unlock()
	b := append([]byte(cwd), 0)
	if uint64(len(b)) > a[1] {
		return ok(-int64(syscall.ERANGE))
	}
	if err := p.mem.CopyOut(a[0], b); err != nil {
		return ok(errno(err))
	}
	return ok(int64(len(b)))
}

func sysChdirFn(p *Process, a [6]uint64) (int64, bool) {
	name, err := p.at(uint64(atFDCWD&0xFFFFFFFF), a[0])
	if err != nil {
		return ok(errno(err))
	}
	return ok(p.chdir(name))
}

func sysFchdirFn(p *Process, a [6]uint64) (int64, bool) {
	f := p.fds.get(a[0])
	if f == nil || f.path == "" {
		return ok(-int64(syscall.EBADF))
	}
	return ok(p.chdir(f.path))
}

func (p *Process) chdir(name string) int64 {
	info, err := fs.Stat(p.sys.files, name)
	if err != nil {
		return errno(err)
	}
	if !info.IsDir() {
		return -int64(syscall.ENOTDIR)
	}
	p.mu.Lock()
	p.cwd = path.Clean("/" + name)
	p.mu.Unlock()
	return 0
}

func sysMkdirFn(p *Process, a [6]uint64) (int64, bool) {
	return ok(p.mkdirAt(uint64(atFDCWD&0xFFFFFFFF), a[0]))
}

func sysMkdiratFn(p *Process, a [6]uint64) (int64, bool) { return ok(p.mkdirAt(a[0], a[1])) }

func (p *Process) mkdirAt(dirfd, nameVA uint64) int64 {
	wfs, isW := p.sys.files.(WritableFS)
	name, err := p.at(dirfd, nameVA)
	switch {
	case err != nil:
		return errno(err)
	case !isW:
		return -int64(syscall.EROFS)
	}
	return errno(wfs.Mkdir(name))
}

func sysUnlinkFn(p *Process, a [6]uint64) (int64, bool) {
	return ok(p.unlinkAt(uint64(atFDCWD&0xFFFFFFFF), a[0], 0))
}

func sysRmdirFn(p *Process, a [6]uint64) (int64, bool) {
	return ok(p.unlinkAt(uint64(atFDCWD&0xFFFFFFFF), a[0], atRemoveDir))
}

func sysUnlinkatFn(p *Process, a [6]uint64) (int64, bool) { return ok(p.unlinkAt(a[0], a[1], a[2])) }

func (p *Process) unlinkAt(dirfd, nameVA, flags uint64) int64 {
	wfs, isW := p.sys.files.(WritableFS)
	name, err := p.at(dirfd, nameVA)
	if err != nil {
		return errno(err)
	}
	info, err := fs.Stat(p.sys.files, name)
	switch {
	case err != nil:
		return errno(err)
	case !isW:
		return -int64(syscall.EROFS)
	case info.IsDir() && flags&atRemoveDir == 0:
		return -int64(syscall.EISDIR)
	case !info.IsDir() && flags&atRemoveDir != 0:
		return -int64(syscall.ENOTDIR)
	}
	if err := wfs.Remove(name); errors.Is(err, fs.ErrExist) {
		return -int64(syscall.ENOTEMPTY) // a directory with entries
	} else if err != nil {
		return errno(err)
	}
	return 0
}
