package proc

import (
	"errors"
	"io/fs"
	"path"
	"strings"
	"syscall"

	"burrow/kernel/vfs"
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
	return ok(p.openAt(uint64(atFDCWD&0xFFFFFFFF), a[0], int(a[1]), uint32(a[2])))
}

func sysOpenatFn(p *Process, a [6]uint64) (int64, bool) {
	return ok(p.openAt(a[0], a[1], int(a[2]), uint32(a[3])))
}

// openAt opens or creates (with mode, less the umask) a file; since W19-S04
// with the permission checks of perm.go (terminals, pseudo-terminals and
// /dev/shm are not files of the tree and are not checked).
func (p *Process) openAt(dirfd, nameVA uint64, flags int, mode uint32) int64 {
	name, err := p.at(dirfd, nameVA)
	if err != nil {
		return errno(err)
	}
	tf, isTTY := p.ttyByPath(name) // the console terminal (W14-S08) or the controlling one
	if isPTYPath(name) {           // a pseudo-terminal (W16-S03)
		if tf, err = p.sys.openPTY(name); err != nil {
			return errno(err)
		}
		tf.path, isTTY = name, true
	}
	if isShmPath(name) { // POSIX shared memory (W18-S03)
		f, err := p.sys.openShm(name, flags)
		if err != nil {
			return errno(err)
		}
		n, err := p.fds.add(f, 0, flags&oCloexec != 0)
		if err != nil {
			f.unref()
			return errno(err)
		}
		return int64(n)
	}
	if isTTY {
		tf.flags = flags & (oAccMode | oNonblock)
		n, err := p.fds.add(tf, 0, flags&oCloexec != 0)
		if err != nil {
			tf.unref()
			return errno(err)
		}
		return int64(n)
	}
	fsys := p.sys.files
	c := p.Cred()
	if err := p.search(&c, name); err != nil {
		return errno(err)
	}
	info, statErr := fs.Stat(fsys, name)
	exists := statErr == nil
	if e := p.mayOpen(&c, name, info, exists, flags); e != 0 {
		return e
	}
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
	case exists && info.Mode().Type() == fs.ModeSocket:
		return -int64(syscall.ENXIO) // a socket node is reached with connect, not open
	case flags&oAccMode == oRDONLY && exists, exists && info.Mode()&fs.ModeDevice != 0:
		if f, err = fsys.Open(name); err != nil { // a device opened for writing writes to it
			return errno(err)
		}
	default:
		if f, err = p.writable(name, exists, flags, p.newAttrs(&c, name, unixToMode(mode), false)); err != nil {
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

// mayOpen checks open's permissions on name (exists: info is its): what
// the flags ask of an existing file (O_EXCL creation of one fails later,
// with EEXIST), or write and search on the directory of a new one. A
// user's write drops a set-user-id bit (and set-group-id with group
// execute), as Linux's first write does.
func (p *Process) mayOpen(c *Cred, name string, info fs.FileInfo, exists bool, flags int) int64 {
	switch {
	case exists && flags&oCreat != 0 && flags&oExcl != 0:
		return 0
	case exists && !c.allows(info, accessWant(flags)):
		return -int64(syscall.EACCES)
	case !exists && flags&oCreat != 0:
		return errno(p.mayCreate(c, name))
	case exists && accessWant(flags)&mayWrite != 0 && !c.root() && info.Mode().IsRegular():
		m := info.Mode()
		drop := m & fs.ModeSetuid
		if m&fs.ModeSetgid != 0 && m&0o010 != 0 {
			drop |= fs.ModeSetgid
		}
		if o, ok := p.owners(); ok && drop != 0 {
			o.Chmod(name, m&(fs.ModePerm|fs.ModeSetuid|fs.ModeSetgid|fs.ModeSticky)&^drop)
		}
	}
	return 0
}

// writable opens (or creates, as n says) a regular file for writing.
func (p *Process) writable(name string, exists bool, flags int, n vfs.NewFile) (fs.File, error) {
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
	} else if err := wfs.WriteFileAs(name, nil, n); err != nil { // create (as n says) or truncate
		return nil, err
	}
	return &wfile{fsys: wfs, name: name, data: data, append: flags&oAppend != 0}, nil
}

// access/faccessat(path, mode): F_OK (0) asks only whether it exists;
// R_OK, W_OK and X_OK are checked with the real ids (W19-S04), as a
// set-user-id program asks what its caller may do.
func sysAccessFn(p *Process, a [6]uint64) (int64, bool) {
	return ok(p.accessAt(uint64(atFDCWD&0xFFFFFFFF), a[0], uint32(a[1]), false))
}

func sysFaccessatFn(p *Process, a [6]uint64) (int64, bool) {
	return ok(p.accessAt(a[0], a[1], uint32(a[2]), false))
}

// faccessat2(dirfd, path, mode, flags): AT_EACCESS checks with the
// effective ids instead.
func sysFaccessat2Fn(p *Process, a [6]uint64) (int64, bool) {
	if a[3]&^atEaccess != 0 {
		return ok(-int64(syscall.EINVAL))
	}
	return ok(p.accessAt(a[0], a[1], uint32(a[2]), a[3]&atEaccess != 0))
}

const atEaccess = 0x200

func (p *Process) accessAt(dirfd, nameVA uint64, mode uint32, effective bool) int64 {
	name, err := p.at(dirfd, nameVA)
	if err != nil {
		return errno(err)
	}
	c := p.Cred()
	if !effective {
		c.UID[idFS], c.GID[idFS] = c.UID[idReal], c.GID[idReal]
	}
	if mode&7 == 0 {
		if err := p.search(&c, name); err != nil {
			return errno(err)
		}
		_, err = fs.Stat(p.sys.files, name)
		return errno(err)
	}
	return errno(p.permit(&c, name, mode&7))
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
	c := p.Cred()
	if err := p.permit(&c, name, mayExec); err != nil {
		return errno(err)
	}
	p.mu.Lock()
	p.cwd = path.Clean("/" + name)
	p.mu.Unlock()
	return 0
}

func sysMkdirFn(p *Process, a [6]uint64) (int64, bool) {
	return ok(p.mkdirAt(uint64(atFDCWD&0xFFFFFFFF), a[0], uint32(a[1])))
}

func sysMkdiratFn(p *Process, a [6]uint64) (int64, bool) {
	return ok(p.mkdirAt(a[0], a[1], uint32(a[2])))
}

func (p *Process) mkdirAt(dirfd, nameVA uint64, mode uint32) int64 {
	wfs, isW := p.sys.files.(WritableFS)
	name, err := p.at(dirfd, nameVA)
	switch {
	case err != nil:
		return errno(err)
	case !isW:
		return -int64(syscall.EROFS)
	}
	c := p.Cred()
	if err := p.mayCreate(&c, name); err != nil {
		return errno(err)
	}
	return errno(wfs.MkdirAs(name, p.newAttrs(&c, name, unixToMode(mode), true)))
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
	if isShmPath(name) { // shm_unlink (W18-S03)
		return p.sys.unlinkShm(name)
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
	c := p.Cred()
	if err := p.mayRemove(&c, name, info); err != nil {
		return errno(err)
	}
	if err := wfs.Remove(name); errors.Is(err, fs.ErrExist) {
		return -int64(syscall.ENOTEMPTY) // a directory with entries
	} else if err != nil {
		return errno(err)
	}
	return 0
}
