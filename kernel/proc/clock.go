package proc

// clock.go - the clocks user programs read (W14-S04): clock_gettime,
// clock_getres, gettimeofday and time. The realtime clocks are wall time:
// the RTC driver sets the nucleus's wall clock at boot, and the Go
// runtime's time.Now reads it. Every other clock counts from boot. Nobody
// may set a clock (clock_settime, settimeofday: -EPERM). Docs:
// POSIX_COVERAGE.md.

import (
	"encoding/binary"
	"syscall"
	"time"
	_ "unsafe" // go:linkname
)

const (
	clockRealtime       = 0
	clockMonotonic      = 1
	clockRealtimeCoarse = 5
	clockRealtimeAlarm  = 8
	clockSGICycle       = 10 // gone from Linux: -EINVAL
	clockTAI            = 11
)

var bootTime = time.Now()

// nanotime is the runtime's monotonic clock: on Burrow the nucleus's, from
// its time_init, which is also what the vDSO counts (W25-S02), so a
// program reads the same clock with and without a system call.
//
//go:linkname nanotime runtime.nanotime
func nanotime() int64

func validClock(id uint64) bool { return id <= clockTAI && id != clockSGICycle }

// clockNow is clock id's reading: since the epoch for the realtime clocks,
// since the nucleus's clock started (boot) for the rest.
func clockNow(id uint64) time.Duration {
	switch id {
	case clockRealtime, clockRealtimeCoarse, clockRealtimeAlarm, clockTAI:
		return time.Duration(time.Now().UnixNano())
	}
	return time.Duration(nanotime())
}

// putTime writes {seconds, d's remainder in unit} at va (a timespec or timeval).
func (p *Process) putTime(va uint64, d, unit time.Duration) int64 {
	var b [16]byte
	binary.LittleEndian.PutUint64(b[0:], uint64(d/time.Second))
	binary.LittleEndian.PutUint64(b[8:], uint64(d%time.Second/unit))
	return errno(p.mem.CopyOut(va, b[:]))
}

func sysClockGettimeFn(p *Process, a [6]uint64) (int64, bool) {
	if !validClock(a[0]) {
		return ok(-int64(syscall.EINVAL))
	}
	return ok(p.putTime(a[1], clockNow(a[0]), time.Nanosecond))
}

// clock_getres: every clock reads the TSC, to the nanosecond.
func sysClockGetresFn(p *Process, a [6]uint64) (int64, bool) {
	if !validClock(a[0]) {
		return ok(-int64(syscall.EINVAL))
	}
	if a[1] == 0 {
		return ok(0)
	}
	return ok(p.putTime(a[1], time.Nanosecond, time.Nanosecond))
}

// gettimeofday(tv, tz): the timezone, if asked for, is UTC.
func sysGettimeofdayFn(p *Process, a [6]uint64) (int64, bool) {
	if a[0] != 0 {
		if e := p.putTime(a[0], clockNow(clockRealtime), time.Microsecond); e < 0 {
			return ok(e)
		}
	}
	if a[1] != 0 {
		return ok(errno(p.mem.CopyOut(a[1], make([]byte, 8)))) // struct timezone {0, 0}
	}
	return ok(0)
}

// time(t): seconds since the epoch, also stored at t unless it is NULL.
func sysTimeFn(p *Process, a [6]uint64) (int64, bool) {
	sec := int64(clockNow(clockRealtime) / time.Second)
	if a[0] != 0 {
		var b [8]byte
		binary.LittleEndian.PutUint64(b[:], uint64(sec))
		if err := p.mem.CopyOut(a[0], b[:]); err != nil {
			return ok(errno(err))
		}
	}
	return ok(sec)
}

func sysSetClockFn(*Process, [6]uint64) (int64, bool) { return ok(-int64(syscall.EPERM)) }

func init() {
	for nr, h := range map[uint64]handler{
		229: sysClockGetresFn, 96: sysGettimeofdayFn, 201: sysTimeFn,
		227: sysSetClockFn, 164: sysSetClockFn, // clock_settime, settimeofday
	} {
		table[nr] = h
	}
}
