package proc

import (
	"encoding/binary"
	"io"
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
	case f == nil || f.flags&oAccMode == oRDONLY:
		return -int64(syscall.EBADF)
	case f.console != nil:
		f.console.Write(b)
		return int64(len(b))
	}
	var n int
	var err error
	switch w := f.f.(type) {
	case Canceller: // a pipe: may block until a reader makes room
		n, err = w.WriteCancel(b, p.cancel())
	case io.Writer: // a device or a writable file
		n, err = w.Write(b)
	default:
		return -int64(syscall.EBADF)
	}
	if err != nil && n == 0 {
		return errno(err)
	}
	return int64(n)
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
	case f == nil || f.flags&oAccMode == oWRONLY:
		return ok(-int64(syscall.EBADF))
	case f.console != nil:
		return ok(0) // console input reaches processes with the TTY (W10-S05): EOF until then
	}
	buf := make([]byte, min(a[2], maxIO))
	var n int
	var err error
	if c, isC := f.f.(Canceller); isC {
		n, err = c.ReadCancel(buf, p.cancel())
	} else {
		n, err = f.f.Read(buf)
	}
	if err != nil && err != io.EOF {
		return ok(errno(err))
	}
	if err := p.mem.CopyOut(a[1], buf[:n]); err != nil {
		return ok(errno(err))
	}
	return ok(int64(n))
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
	return ok(p.sleep(time.Duration(sec)*time.Second + time.Duration(nsec)))
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
