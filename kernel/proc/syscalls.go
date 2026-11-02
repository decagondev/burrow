package proc

import (
	"encoding/binary"
	"io"
	"io/fs"
	"strings"
	"syscall"
	"time"
)

// Linux x86-64 numbers of the calls implemented (docs/02-architecture/POSIX_COVERAGE.md).
const (
	sysRead          = 0
	sysWrite         = 1
	sysOpen          = 2
	sysClose         = 3
	sysFstat         = 5
	sysLseek         = 8
	sysMmap          = 9
	sysMunmap        = 11
	sysBrk           = 12
	sysRtSigaction   = 13
	sysRtSigprocmask = 14
	sysIoctl         = 16
	sysWritev        = 20
	sysNanosleep     = 35
	sysGetpid        = 39
	sysClone         = 56
	sysFork          = 57
	sysVfork         = 58
	sysExecve        = 59
	sysExit          = 60
	sysWait4         = 61
	sysGetppid       = 110
	sysGetuid        = 102
	sysGetgid        = 104
	sysGeteuid       = 107
	sysGetegid       = 108
	sysArchPrctl     = 158
	sysGettid        = 186
	sysSetTidAddress = 218
	sysClockGettime  = 228
	sysExitGroup     = 231
	sysOpenat        = 257
)

// handler runs one call; exited means the process is gone (no reply).
type handler func(p *Process, a [6]uint64) (res int64, exited bool)

func ok(r int64) (int64, bool) { return r, false }

var table = map[uint64]handler{
	sysRead: sysReadFn, sysWrite: sysWriteFn, sysOpen: sysOpenFn, sysClose: sysCloseFn,
	sysFstat: sysFstatFn, sysLseek: sysLseekFn, sysMmap: sysMmapFn, sysMunmap: sysMunmapFn,
	sysBrk: sysBrkFn, sysIoctl: sysIoctlFn, sysWritev: sysWritevFn, sysNanosleep: sysNanosleepFn,
	sysArchPrctl: sysArchPrctlFn, sysClockGettime: sysClockGettimeFn, sysOpenat: sysOpenatFn,
	sysExit: sysExitFn, sysExitGroup: sysExitFn,
	sysFork: sysForkFn, sysVfork: sysForkFn, sysClone: sysCloneFn, sysExecve: sysExecveFn,
	sysWait4: sysWait4Fn, sysGetppid: sysGetppidFn,
	sysGetpid: pidFn, sysGettid: pidFn, sysSetTidAddress: pidFn, // one thread: tid = pid
	sysGetuid: zeroFn, sysGetgid: zeroFn, sysGeteuid: zeroFn, sysGetegid: zeroFn, // root
	sysRtSigaction: zeroFn, sysRtSigprocmask: zeroFn, // accepted; signals come in W10
}

func (p *Process) syscall(nr uint64, a [6]uint64) (int64, bool) {
	h, found := table[nr]
	if !found {
		p.sys.unknown(p, nr)
		return ok(-int64(syscall.ENOSYS))
	}
	return h(p, a)
}

func pidFn(p *Process, _ [6]uint64) (int64, bool) { return ok(int64(p.PID)) }
func zeroFn(*Process, [6]uint64) (int64, bool)    { return ok(0) }

func sysExitFn(p *Process, a [6]uint64) (int64, bool) {
	p.exit(int(a[0]&0xFF), 0, "")
	return 0, true
}

const maxIO = 1 << 20 // one read or write moves at most this much

func sysWriteFn(p *Process, a [6]uint64) (int64, bool) {
	buf := make([]byte, min(a[2], maxIO))
	if err := p.mem.CopyIn(a[1], buf); err != nil {
		return ok(errno(err))
	}
	return ok(p.write(a[0], buf))
}

func (p *Process) write(fd uint64, b []byte) int64 {
	f := p.fds.get(fd)
	switch {
	case f == nil:
		return -int64(syscall.EBADF)
	case f.console != nil:
		f.console.Write(b)
		return int64(len(b))
	}
	if w, isW := f.f.(io.Writer); isW { // device files (devfs)
		n, err := w.Write(b)
		if err != nil {
			return errno(err)
		}
		return int64(n)
	}
	return -int64(syscall.EBADF) // opened read-only: files are read-only in W9
}

// writev(fd, iov, iovcnt): struct iovec { void *base; size_t len; }.
func sysWritevFn(p *Process, a [6]uint64) (int64, bool) {
	if a[2] > 1024 {
		return ok(-int64(syscall.EINVAL))
	}
	iov := make([]byte, 16*a[2])
	if err := p.mem.CopyIn(a[1], iov); err != nil {
		return ok(errno(err))
	}
	var all []byte
	for i := uint64(0); i < a[2]; i++ {
		base, n := binary.LittleEndian.Uint64(iov[16*i:]), binary.LittleEndian.Uint64(iov[16*i+8:])
		if uint64(len(all))+n > maxIO {
			return ok(-int64(syscall.EINVAL))
		}
		b := make([]byte, n)
		if err := p.mem.CopyIn(base, b); err != nil {
			return ok(errno(err))
		}
		all = append(all, b...)
	}
	return ok(p.write(a[0], all))
}

func sysReadFn(p *Process, a [6]uint64) (int64, bool) {
	f := p.fds.get(a[0])
	switch {
	case f == nil:
		return ok(-int64(syscall.EBADF))
	case f.console != nil:
		return ok(0) // console input reaches processes with the TTY (W10): EOF until then
	}
	buf := make([]byte, min(a[2], maxIO))
	n, err := f.f.Read(buf)
	if err != nil && err != io.EOF {
		return ok(errno(err))
	}
	if err := p.mem.CopyOut(a[1], buf[:n]); err != nil {
		return ok(errno(err))
	}
	return ok(int64(n))
}

const (
	oAccMode = 3
	oCloexec = 0x80000
	atFDCWD  = -100
)

func sysOpenFn(p *Process, a [6]uint64) (int64, bool) { return ok(p.open(a[0], a[1])) }

func sysOpenatFn(p *Process, a [6]uint64) (int64, bool) {
	if int32(a[0]) != atFDCWD {
		name, err := p.mem.CString(a[1], 4096)
		if err != nil || !strings.HasPrefix(name, "/") {
			return ok(-int64(syscall.ENOTSUP)) // directory fds come with W10's openat family
		}
	}
	return ok(p.open(a[1], a[2]))
}

// open: read-only for now; relative paths start at / (no cwd until W10).
func (p *Process) open(nameVA, flags uint64) int64 {
	name, err := p.mem.CString(nameVA, 4096)
	if err != nil {
		return errno(err)
	}
	if flags&oAccMode != 0 {
		return -int64(syscall.EROFS)
	}
	f, err := p.sys.files.Open(p.resolve(name))
	if err != nil {
		return errno(err)
	}
	fd, err := p.fds.add(newFile(f, nil), 0, flags&oCloexec != 0)
	if err != nil {
		f.Close()
		return errno(err)
	}
	return int64(fd)
}

func sysCloseFn(p *Process, a [6]uint64) (int64, bool) { return ok(errno(p.fds.close(a[0]))) }

func sysLseekFn(p *Process, a [6]uint64) (int64, bool) {
	f := p.fds.get(a[0])
	if f == nil {
		return ok(-int64(syscall.EBADF))
	}
	s, isSeeker := f.f.(io.Seeker)
	if !isSeeker {
		return ok(-int64(syscall.ESPIPE))
	}
	off, err := s.Seek(int64(a[1]), int(a[2]))
	if err != nil {
		return ok(errno(err))
	}
	return ok(off)
}

// fstat fills Linux's struct stat (144 bytes).
func sysFstatFn(p *Process, a [6]uint64) (int64, bool) {
	f := p.fds.get(a[0])
	if f == nil {
		return ok(-int64(syscall.EBADF))
	}
	st := make([]byte, 144)
	put := func(off int, v uint64) { binary.LittleEndian.PutUint64(st[off:], v) }
	mode := uint64(syscall.S_IFCHR | 0o620) // the console
	if f.f != nil {
		info, err := f.f.Stat()
		if err != nil {
			return ok(errno(err))
		}
		mode = unixMode(info.Mode())
		put(48, uint64(info.Size()))
		put(88, uint64(info.ModTime().Unix())) // st_mtim.tv_sec
	}
	put(16, 1)                                           // st_nlink
	binary.LittleEndian.PutUint32(st[24:], uint32(mode)) // st_mode
	put(56, PageSize)                                    // st_blksize
	return ok(errno(p.mem.CopyOut(a[1], st)))
}

func unixMode(m fs.FileMode) uint64 {
	t := uint64(syscall.S_IFREG)
	switch {
	case m.IsDir():
		t = syscall.S_IFDIR
	case m&fs.ModeCharDevice != 0:
		t = syscall.S_IFCHR
	}
	return t | uint64(m.Perm())
}

// ioctl: no TTY until W10, so TCGETS and TIOCGWINSZ say "not a terminal"
// (musl then buffers stdout fully and flushes at exit).
func sysIoctlFn(p *Process, a [6]uint64) (int64, bool) {
	if p.fds.get(a[0]) == nil {
		return ok(-int64(syscall.EBADF))
	}
	return ok(-int64(syscall.ENOTTY))
}

const (
	archSetFS = 0x1002
	archGetFS = 0x1003
)

func sysArchPrctlFn(p *Process, a [6]uint64) (int64, bool) {
	switch a[0] {
	case archSetFS:
		if err := p.sys.nu.SetFS(p.tid, a[1]); err != nil {
			return ok(errno(err))
		}
		p.mu.Lock()
		p.fs = a[1]
		p.mu.Unlock()
		return ok(0)
	case archGetFS:
		p.mu.Lock()
		fs := p.fs
		p.mu.Unlock()
		var b [8]byte
		binary.LittleEndian.PutUint64(b[:], fs)
		return ok(errno(p.mem.CopyOut(a[1], b[:])))
	}
	return ok(-int64(syscall.EINVAL))
}

var bootTime = time.Now()

// clock_gettime: CLOCK_REALTIME is time since boot (no RTC driver yet).
func sysClockGettimeFn(p *Process, a [6]uint64) (int64, bool) {
	if a[0] > 7 {
		return ok(-int64(syscall.EINVAL))
	}
	d := time.Since(bootTime)
	var ts [16]byte
	binary.LittleEndian.PutUint64(ts[0:], uint64(d/time.Second))
	binary.LittleEndian.PutUint64(ts[8:], uint64(d%time.Second))
	return ok(errno(p.mem.CopyOut(a[1], ts[:])))
}

func sysNanosleepFn(p *Process, a [6]uint64) (int64, bool) {
	var ts [16]byte
	if err := p.mem.CopyIn(a[0], ts[:]); err != nil {
		return ok(errno(err))
	}
	sec, nsec := int64(binary.LittleEndian.Uint64(ts[0:])), int64(binary.LittleEndian.Uint64(ts[8:]))
	if sec < 0 || nsec < 0 || nsec >= 1e9 {
		return ok(-int64(syscall.EINVAL))
	}
	time.Sleep(time.Duration(sec)*time.Second + time.Duration(nsec))
	return ok(0)
}

func sysGetppidFn(p *Process, _ [6]uint64) (int64, bool) {
	p.sys.mu.Lock()
	defer p.sys.mu.Unlock()
	return ok(int64(p.PPID))
}

func sysForkFn(p *Process, _ [6]uint64) (int64, bool) { return ok(p.fork()) }

const (
	cloneVM    = 0x100
	cloneVfork = 0x4000
	exitSignal = 0xFF // low byte of clone's flags: the signal sent at exit
)

// clone: only the fork shape (no shared memory) until threads come (W12).
func sysCloneFn(p *Process, a [6]uint64) (int64, bool) {
	if a[0]&^(exitSignal|cloneVfork) != 0 {
		return ok(-int64(syscall.ENOSYS))
	}
	return ok(p.fork())
}

func sysExecveFn(p *Process, a [6]uint64) (int64, bool) { return ok(p.execve(a[0], a[1], a[2])) }

func sysWait4Fn(p *Process, a [6]uint64) (int64, bool) {
	return ok(p.wait4(int64(int32(a[0])), a[1], a[2]))
}
