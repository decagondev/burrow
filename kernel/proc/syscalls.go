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
	sysReadv         = 19
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
	sysPrctl         = 157
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
	sysOpen: sysOpenFn, sysClose: sysCloseFn,
	sysFstat: sysFstatFn, sysLseek: sysLseekFn, sysMmap: sysMmapFn, sysMunmap: sysMunmapFn,
	sysBrk: sysBrkFn, sysPrctl: sysPrctlFn, sysClockGettime: sysClockGettimeFn, sysOpenat: sysOpenatFn,
	sysGetppid: sysGetppidFn, sysGetpid: pidFn,
	sysGetuid: zeroFn, sysGetgid: zeroFn, sysGeteuid: zeroFn, sysGetegid: zeroFn, // root
}

// The calls that need the calling thread (thread.go has the rest).
func init() {
	for nr, h := range map[uint64]thandler{
		sysRead: sysReadFn, sysWrite: sysWriteFn, sysReadv: sysReadvFn, sysWritev: sysWritevFn,
		sysNanosleep: sysNanosleepFn, sysArchPrctl: sysArchPrctlFn,
		sysFork: sysForkFn, sysVfork: sysForkFn, sysClone: sysCloneFn, sysExecve: sysExecveFn,
		sysWait4: sysWait4Fn,
	} {
		ttable[nr] = h
	}
}

func pidFn(p *Process, _ [6]uint64) (int64, bool) { return ok(int64(p.PID)) }
func zeroFn(*Process, [6]uint64) (int64, bool)    { return ok(0) }

const maxIO = 1 << 20 // one read or write moves at most this much

func sysWriteFn(t *Thread, a [6]uint64) (int64, bool) {
	p := t.p
	buf := make([]byte, min(a[2], maxIO))
	if err := p.mem.CopyIn(a[1], buf); err != nil {
		return ok(errno(err))
	}
	return ok(t.write(a[0], buf))
}

// write is one write(2); EPIPE also raises SIGPIPE at the caller, as on
// Linux (BUG-0015: before W14-S06 pipes returned EPIPE silently).
func (t *Thread) write(fd uint64, b []byte) int64 {
	r := t.writeFile(fd, b)
	if r == -int64(syscall.EPIPE) {
		t.send(sigPIPE, sigInfo{signo: sigPIPE, code: siKernel})
	}
	return r
}

func (t *Thread) writeFile(fd uint64, b []byte) int64 {
	p := t.p
	f := p.fds.get(fd)
	switch {
	case f == nil || f.flags&oAccMode == oRDONLY:
		return -int64(syscall.EBADF)
	case f.console != nil:
		f.console.Write(b)
		return int64(len(b))
	}
	if wouldBlock(f, pollOut) {
		return -int64(syscall.EAGAIN)
	}
	var n int
	var err error
	switch w := device(f).(type) {
	case *sockEnd: // a socket: the chunk carries the writer's pid (SCM_CREDENTIALS)
		n, err = w.sendFrom(b, int32(p.PID), t.cancel())
	case Canceller: // a pipe: may block until a reader makes room
		n, err = w.WriteCancel(b, t.cancel())
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
func sysWritevFn(t *Thread, a [6]uint64) (int64, bool) {
	p := t.p
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
	return ok(t.write(a[0], all))
}

func sysReadFn(t *Thread, a [6]uint64) (int64, bool) {
	p := t.p
	b, e := t.readFile(a[0], a[2])
	if e < 0 {
		return ok(e)
	}
	if err := p.mem.CopyOut(a[1], b); err != nil {
		return ok(errno(err))
	}
	return ok(int64(len(b)))
}

// readv: one read of the iovecs' total length, scattered over them (W11:
// musl's stdio reads with it).
func sysReadvFn(t *Thread, a [6]uint64) (int64, bool) {
	p := t.p
	if a[2] > 1024 {
		return ok(-int64(syscall.EINVAL))
	}
	iov := make([]byte, 16*a[2])
	if err := p.mem.CopyIn(a[1], iov); err != nil {
		return ok(errno(err))
	}
	var total uint64
	for i := uint64(0); i < a[2]; i++ {
		total += binary.LittleEndian.Uint64(iov[16*i+8:])
	}
	if total > maxIO {
		total = maxIO
	}
	b, e := t.readFile(a[0], total)
	if e < 0 {
		return ok(e)
	}
	n := len(b)
	for i := uint64(0); i < a[2] && len(b) > 0; i++ {
		base, size := binary.LittleEndian.Uint64(iov[16*i:]), binary.LittleEndian.Uint64(iov[16*i+8:])
		k := min(uint64(len(b)), size)
		if err := p.mem.CopyOut(base, b[:k]); err != nil {
			return ok(errno(err))
		}
		b = b[k:]
	}
	return ok(int64(n))
}

// readFile is one read(2) of at most n bytes from fd: the bytes, or -errno.
func (t *Thread) readFile(fd, n uint64) ([]byte, int64) {
	p := t.p
	f := p.fds.get(fd)
	switch {
	case f == nil || f.flags&oAccMode == oWRONLY:
		return nil, -int64(syscall.EBADF)
	case f.console != nil:
		return nil, 0 // console input reaches processes with the TTY (W10-S05): EOF until then
	}
	if wouldBlock(f, pollIn) {
		return nil, -int64(syscall.EAGAIN)
	}
	buf := make([]byte, min(n, maxIO))
	var k int
	var err error
	if c, isC := device(f).(Canceller); isC {
		k, err = c.ReadCancel(buf, t.cancel())
	} else {
		k, err = f.f.Read(buf)
	}
	if err != nil && err != io.EOF {
		return nil, errno(err)
	}
	return buf[:k], 0
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

func sysNanosleepFn(t *Thread, a [6]uint64) (int64, bool) {
	p := t.p
	var ts [16]byte
	if err := p.mem.CopyIn(a[0], ts[:]); err != nil {
		return ok(errno(err))
	}
	sec, nsec := int64(binary.LittleEndian.Uint64(ts[0:])), int64(binary.LittleEndian.Uint64(ts[8:]))
	if sec < 0 || nsec < 0 || nsec >= 1e9 {
		return ok(-int64(syscall.EINVAL))
	}
	return ok(t.sleep(time.Duration(sec)*time.Second + time.Duration(nsec)))
}

func sysGetppidFn(p *Process, _ [6]uint64) (int64, bool) {
	p.sys.mu.Lock()
	defer p.sys.mu.Unlock()
	return ok(int64(p.PPID))
}

func sysForkFn(t *Thread, _ [6]uint64) (int64, bool) { return ok(t.fork()) }

func sysExecveFn(t *Thread, a [6]uint64) (int64, bool) { return ok(t.execve(a[0], a[1], a[2])) }

func sysWait4Fn(t *Thread, a [6]uint64) (int64, bool) {
	return ok(t.wait4(int64(int32(a[0])), a[1], a[2]))
}
