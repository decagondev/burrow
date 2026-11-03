package proc

// startcalls.go - the small calls that start-up code makes (W14-S03):
// getrandom, sysinfo, umask, getrusage, the scheduler queries and
// membarrier. Each answers what Linux answers on a one-CPU machine where
// everyone is root. getrandom reads kernel/random, which must never be
// taken for a cryptographic source. Docs: POSIX_COVERAGE.md.

import (
	"encoding/binary"
	"syscall"
	"time"

	"burrow/kernel/random"
)

const (
	grndNonblock = 1
	grndRandom   = 2
	grndInsecure = 4

	defaultUmask = 0o022
	sysinfoBytes = 112 // struct sysinfo
	rusageBytes  = 144 // struct rusage
	rusageSelf   = 0
	rusageThread = 1
	rusageKids   = -1

	membarrierQuery = 0
)

// getrandom(buf, count, flags): never blocks (the generator is seeded
// before any process starts).
func sysGetrandomFn(p *Process, a [6]uint64) (int64, bool) {
	if a[2]&^(grndNonblock|grndRandom|grndInsecure) != 0 || a[2]&grndRandom != 0 && a[2]&grndInsecure != 0 {
		return ok(-int64(syscall.EINVAL))
	}
	b := make([]byte, min(a[1], maxIO))
	random.Kernel.Read(b)
	if err := p.mem.CopyOut(a[0], b); err != nil {
		return ok(errno(err))
	}
	return ok(int64(len(b)))
}

// sysinfo(info): uptime, RAM from the nucleus, the process count; no swap
// and no load average.
func sysSysinfoFn(p *Process, a [6]uint64) (int64, bool) {
	total, free := p.sys.nu.MemInfo()
	p.sys.mu.Lock()
	procs := len(p.sys.byPID)
	p.sys.mu.Unlock()
	le := binary.LittleEndian
	b := make([]byte, sysinfoBytes)
	le.PutUint64(b[0:], uint64(time.Since(bootTime)/time.Second))
	le.PutUint64(b[32:], total)
	le.PutUint64(b[40:], free)
	le.PutUint16(b[80:], uint16(min(procs, 0xFFFF)))
	le.PutUint32(b[104:], 1) // mem_unit: the sizes are in bytes
	return ok(errno(p.mem.CopyOut(a[0], b)))
}

// umask(mask): the old mask; fork inherits it.
func sysUmaskFn(p *Process, a [6]uint64) (int64, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	old := p.umask
	p.umask = uint32(a[0]) & 0o777
	return ok(int64(old))
}

// getrusage(who, usage): nothing is accounted yet, so all zeros.
func sysGetrusageFn(p *Process, a [6]uint64) (int64, bool) {
	switch int32(a[0]) {
	case rusageSelf, rusageThread, rusageKids:
		return ok(errno(p.mem.CopyOut(a[1], make([]byte, rusageBytes))))
	}
	return ok(-int64(syscall.EINVAL))
}

// schedTarget checks a scheduler call's pid: 0 (the caller), or any live
// process or thread; -ESRCH otherwise.
func (p *Process) schedTarget(pid uint64) int64 {
	n := int32(pid)
	if n < 0 {
		return -int64(syscall.EINVAL)
	}
	if n == 0 || int(n) == p.PID {
		return 0
	}
	p.mu.Lock()
	_, mine := p.threads[int(n)]
	p.mu.Unlock()
	p.sys.mu.Lock()
	q, live := p.sys.byPID[int(n)]
	live = live && !q.zombie
	p.sys.mu.Unlock()
	if mine || live {
		return 0
	}
	return -int64(syscall.ESRCH)
}

// sched_getparam(pid, param): priority 0, as SCHED_OTHER has.
func sysSchedGetparamFn(p *Process, a [6]uint64) (int64, bool) {
	if e := p.schedTarget(a[0]); e < 0 {
		return ok(e)
	}
	return ok(errno(p.mem.CopyOut(a[1], make([]byte, 4))))
}

// sched_getscheduler(pid): SCHED_OTHER (0).
func sysSchedGetschedulerFn(p *Process, a [6]uint64) (int64, bool) { return ok(p.schedTarget(a[0])) }

// sched_setaffinity(pid, len, mask): accepted when the mask holds CPU 0,
// the only one.
func sysSchedSetaffinityFn(p *Process, a [6]uint64) (int64, bool) {
	if e := p.schedTarget(a[0]); e < 0 {
		return ok(e)
	}
	if a[1] == 0 {
		return ok(-int64(syscall.EINVAL))
	}
	var b [1]byte
	if err := p.mem.CopyIn(a[2], b[:]); err != nil {
		return ok(errno(err))
	}
	if b[0]&1 == 0 {
		return ok(-int64(syscall.EINVAL))
	}
	return ok(0)
}

// membarrier(cmd, flags, cpu): no barrier commands; QUERY says so.
func sysMembarrierFn(p *Process, a [6]uint64) (int64, bool) {
	if a[0] == membarrierQuery && a[1] == 0 {
		return ok(0)
	}
	return ok(-int64(syscall.EINVAL))
}

func init() {
	for nr, h := range map[uint64]handler{
		318: sysGetrandomFn, 99: sysSysinfoFn, 95: sysUmaskFn, 98: sysGetrusageFn,
		143: sysSchedGetparamFn, 145: sysSchedGetschedulerFn, 203: sysSchedSetaffinityFn,
		324: sysMembarrierFn,
	} {
		table[nr] = h
	}
}
