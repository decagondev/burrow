package proc

// cputime.go - CPU time (W23-S02): the nucleus counts each thread's user
// and kernel time; a process's is its live threads' plus what its ended
// threads had (read just before each is killed), fixed at exit; a parent
// adds a child's (and the child's children's) when it reaps it. times(2),
// getrusage(2) and the rusage of wait4 and waitid report them, in Linux's
// layouts. Time spent in Go serving a call is not charged to the caller
// (the nucleus thread waits, unscheduled). Docs: WAVE-23-proc-virtio.md.

import (
	"encoding/binary"
	"syscall"
	"time"
)

// CPUTime is one CPU's time (nu_cpu_times).
type CPUTime struct {
	User, Sys, Idle time.Duration
	Switches        uint64
}

// cpuTimes is user and system time.
type cpuTimes struct{ user, sys time.Duration }

func (a cpuTimes) plus(b cpuTimes) cpuTimes { return cpuTimes{a.user + b.user, a.sys + b.sys} }

const (
	clockTicks = 100 // USER_HZ
	tmsBytes   = 32  // struct tms: four clock_t
)

// times is a thread's time; zero once the nucleus has let it go.
func (t *Thread) times() cpuTimes {
	u, s, err := t.p.sys.nu.ThreadTimes(t.nt)
	if err != nil {
		return cpuTimes{}
	}
	return cpuTimes{u, s}
}

// cpu is the process's own time: fixed once it has exited.
func (p *Process) cpu() cpuTimes {
	p.mu.Lock()
	if p.exited {
		defer p.mu.Unlock()
		return p.final
	}
	total, threads := p.ended, make([]*Thread, 0, len(p.threads))
	for _, t := range p.threads {
		threads = append(threads, t)
	}
	p.mu.Unlock()
	for _, t := range threads {
		total = total.plus(t.times())
	}
	return total
}

// endThreads keeps the time of threads about to be killed.
func (p *Process) endThreads(threads []*Thread) {
	var sum cpuTimes
	for _, t := range threads {
		sum = sum.plus(t.times())
	}
	p.mu.Lock()
	p.ended = p.ended.plus(sum)
	p.mu.Unlock()
}

// reaped is c's time and peak memory going to its parent p (wait4, waitid).
func (p *Process) reaped(c *Process) {
	p.mu.Lock()
	p.kids = p.kids.plus(c.final).plus(c.kids)
	p.kidsRSS = max(p.kidsRSS, c.finalRSS, c.kidsRSS)
	p.mu.Unlock()
}

// childTimes is the reaped children's time and peak memory.
func (p *Process) childTimes() (cpuTimes, uint64) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.kids, p.kidsRSS
}

func ticks(d time.Duration) uint64 { return uint64(d / (time.Second / clockTicks)) }

// rusage is struct rusage: the two times as timevals and ru_maxrss in KiB.
func rusage(c cpuTimes, rssBytes uint64) []byte {
	b := make([]byte, rusageBytes)
	le := binary.LittleEndian
	for i, d := range []time.Duration{c.user, c.sys} {
		le.PutUint64(b[16*i:], uint64(d/time.Second))
		le.PutUint64(b[16*i+8:], uint64(d%time.Second/time.Microsecond))
	}
	le.PutUint64(b[32:], rssBytes>>10)
	return b
}

// rss is the bytes of memory the process has now.
func (p *Process) rss() uint64 {
	p.mu.Lock()
	mem := p.mem
	p.mu.Unlock()
	if mem == nil {
		return 0
	}
	_, r := mem.usage()
	return r
}

// times(buf): the caller's and its reaped children's times in ticks, and
// the ticks since boot.
func sysTimesFn(p *Process, a [6]uint64) (int64, bool) {
	if a[0] != 0 {
		own := p.cpu()
		kids, _ := p.childTimes()
		b := make([]byte, tmsBytes)
		for i, d := range []time.Duration{own.user, own.sys, kids.user, kids.sys} {
			binary.LittleEndian.PutUint64(b[8*i:], ticks(d))
		}
		if err := p.mem.CopyOut(a[0], b); err != nil {
			return ok(errno(err))
		}
	}
	return ok(int64(ticks(time.Since(bootTime))))
}

// getrusage(who, usage): RUSAGE_SELF, RUSAGE_THREAD or RUSAGE_CHILDREN.
func sysGetrusageFn(t *Thread, a [6]uint64) (int64, bool) {
	p := t.p
	var b []byte
	switch int32(a[0]) {
	case rusageSelf:
		b = rusage(p.cpu(), p.rss())
	case rusageThread:
		b = rusage(t.times(), p.rss())
	case rusageKids:
		b = rusage(p.childTimes())
	default:
		return ok(-int64(syscall.EINVAL))
	}
	return ok(errno(p.mem.CopyOut(a[1], b)))
}

// childUsage writes a reaped child's usage for wait4 and waitid (va 0: none).
func (p *Process) childUsage(c *Process, va uint64) error {
	if va == 0 {
		return nil
	}
	return p.mem.CopyOut(va, rusage(c.final.plus(c.kids), max(c.finalRSS, c.kidsRSS)))
}

func init() {
	table[100] = sysTimesFn
	ttable[98] = sysGetrusageFn
}
