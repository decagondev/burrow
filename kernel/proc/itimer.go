package proc

// itimer.go - interval timers (W16-S02: TinyX's scheduler runs on SIGALRM):
// setitimer, getitimer and alarm. ITIMER_REAL counts wall time and sends
// SIGALRM, again every interval when one is set; a Go timer does the
// waiting. ITIMER_VIRTUAL and ITIMER_PROF are kept and reported but never
// fire: Burrow does not measure a process's CPU time. As on Linux, timers
// survive execve, a fork's child starts with none, and exit stops them.
// Docs: docs/02-architecture/POSIX_COVERAGE.md.

import (
	"encoding/binary"
	"syscall"
	"time"
)

const (
	itimerReal    = 0
	itimerVirtual = 1
	itimerProf    = 2
	sigALRM       = 14
	itimervalLen  = 32 // struct itimerval {timeval interval, value}
)

// itimer is one interval timer; p.mu guards it.
type itimer struct {
	t        *time.Timer // ITIMER_REAL only
	due      time.Time   // when it next fires; zero when disarmed
	interval time.Duration
	value    time.Duration // the CPU-time timers: kept, never counted down
}

// arm sets p's timer which to fire after value, then every interval; it
// returns what was left of the old one (p.mu held).
func (p *Process) arm(which int, value, interval time.Duration) (oldValue, oldInterval time.Duration) {
	it := &p.timers[which]
	oldInterval = it.interval
	if which != itimerReal {
		oldValue, it.value, it.interval = it.value, value, interval
		return oldValue, oldInterval
	}
	if !it.due.IsZero() {
		oldValue = max(time.Until(it.due), time.Microsecond) // still armed: never report 0
	}
	if it.t != nil {
		it.t.Stop()
		it.t = nil
	}
	it.due, it.interval = time.Time{}, interval
	if value > 0 {
		it.due = time.Now().Add(value)
		it.t = time.AfterFunc(value, p.alarm)
	}
	return oldValue, oldInterval
}

// alarm is ITIMER_REAL firing: SIGALRM, and the next round when periodic.
func (p *Process) alarm() {
	p.mu.Lock()
	it := &p.timers[itimerReal]
	gone := p.exited
	if !gone && it.interval > 0 {
		it.due = time.Now().Add(it.interval)
		it.t = time.AfterFunc(it.interval, p.alarm)
	} else {
		it.due, it.t = time.Time{}, nil
	}
	p.mu.Unlock()
	if !gone {
		p.send(sigALRM, sigInfo{signo: sigALRM, code: siKernel})
	}
}

// stopTimers is exit's: nothing fires for a dead process.
func (p *Process) stopTimers() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if t := p.timers[itimerReal].t; t != nil {
		t.Stop()
	}
	p.timers = [3]itimer{}
}

func timeval(b []byte) (time.Duration, bool) {
	sec, usec := int64(binary.LittleEndian.Uint64(b)), int64(binary.LittleEndian.Uint64(b[8:]))
	if sec < 0 || usec < 0 || usec >= 1e6 {
		return 0, false
	}
	return time.Duration(sec)*time.Second + time.Duration(usec)*time.Microsecond, true
}

func putTimeval(b []byte, d time.Duration) {
	binary.LittleEndian.PutUint64(b, uint64(d/time.Second))
	binary.LittleEndian.PutUint64(b[8:], uint64(d%time.Second/time.Microsecond))
}

// remaining is what getitimer reports for which (p.mu held).
func (p *Process) remaining(which int) (time.Duration, time.Duration) {
	it := p.timers[which]
	if which != itimerReal {
		return it.value, it.interval
	}
	if it.due.IsZero() {
		return 0, it.interval
	}
	return max(time.Until(it.due), time.Microsecond), it.interval
}

// setitimer(which, new, old).
func sysSetitimerFn(p *Process, a [6]uint64) (int64, bool) {
	which := int(a[0])
	if which < itimerReal || which > itimerProf {
		return ok(-int64(syscall.EINVAL))
	}
	var value, interval time.Duration
	if a[1] != 0 {
		b := make([]byte, itimervalLen)
		if err := p.mem.CopyIn(a[1], b); err != nil {
			return ok(errno(err))
		}
		var ok1, ok2 bool
		interval, ok1 = timeval(b[0:])
		value, ok2 = timeval(b[16:])
		if !ok1 || !ok2 {
			return ok(-int64(syscall.EINVAL))
		}
	}
	p.mu.Lock()
	oldValue, oldInterval := p.arm(which, value, interval)
	p.mu.Unlock()
	if a[2] != 0 {
		b := make([]byte, itimervalLen)
		putTimeval(b[0:], oldInterval)
		putTimeval(b[16:], oldValue)
		return ok(errno(p.mem.CopyOut(a[2], b)))
	}
	return ok(0)
}

// getitimer(which, cur).
func sysGetitimerFn(p *Process, a [6]uint64) (int64, bool) {
	which := int(a[0])
	if which < itimerReal || which > itimerProf {
		return ok(-int64(syscall.EINVAL))
	}
	p.mu.Lock()
	value, interval := p.remaining(which)
	p.mu.Unlock()
	b := make([]byte, itimervalLen)
	putTimeval(b[0:], interval)
	putTimeval(b[16:], value)
	return ok(errno(p.mem.CopyOut(a[1], b)))
}

// alarm(seconds): ITIMER_REAL once; the seconds left on the old one, rounded.
func sysAlarmFn(p *Process, a [6]uint64) (int64, bool) {
	p.mu.Lock()
	oldValue, _ := p.arm(itimerReal, time.Duration(uint32(a[0]))*time.Second, 0)
	p.mu.Unlock()
	return ok(int64((oldValue + time.Second/2) / time.Second))
}

func init() {
	for nr, h := range map[uint64]handler{38: sysSetitimerFn, 36: sysGetitimerFn, 37: sysAlarmFn} {
		table[nr] = h
	}
}
