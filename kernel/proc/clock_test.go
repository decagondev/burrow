package proc_test

import (
	"syscall"
	"testing"
	"time"
)

// The realtime clocks are wall time (the host's here, the RTC's on
// Burrow); the monotonic one counts from boot.
func TestClocks(t *testing.T) {
	r := newRig(t)
	p := r.spawn()
	now := time.Now().Unix()
	for _, id := range []uint64{0, 5, 11} { // REALTIME, REALTIME_COARSE, TAI
		if e := r.call(p, 228, id, scratch); e != 0 {
			t.Fatalf("clock_gettime(%d) = %d", id, e)
		}
		if sec := int64(le.Uint64(read(p, scratch, 8))); sec < now || sec > now+1 {
			t.Errorf("clock %d: %d s, want about %d", id, sec, now)
		}
	}
	r.call(p, 228, 1, scratch)
	if sec := le.Uint64(read(p, scratch, 8)); sec > 3600 {
		t.Errorf("CLOCK_MONOTONIC = %d s: not counting from boot", sec)
	}
	if e := r.call(p, 228, 10, scratch); e != -int64(syscall.EINVAL) {
		t.Errorf("clock 10 = %d, want EINVAL", e)
	}
	if e := r.call(p, 229, 1, scratch); e != 0 || le.Uint64(read(p, scratch, 8)) != 0 || le.Uint64(read(p, scratch+8, 8)) != 1 {
		t.Errorf("clock_getres = %d", e)
	}
	if e := r.call(p, 96, scratch, scratch+16); e != 0 {
		t.Fatalf("gettimeofday = %d", e)
	}
	sec, usec := int64(le.Uint64(read(p, scratch, 8))), le.Uint64(read(p, scratch+8, 8))
	if sec < now || sec > now+1 || usec >= 1e6 {
		t.Errorf("gettimeofday %d.%06d", sec, usec)
	}
	if n := r.call(p, 201, scratch); n < now || n > now+1 || int64(le.Uint64(read(p, scratch, 8))) != n {
		t.Errorf("time = %d", n)
	}
	if e := r.call(p, 227, 0, scratch); e != -int64(syscall.EPERM) {
		t.Errorf("clock_settime = %d, want EPERM", e)
	}
}

// FUTEX_WAIT_BITSET with FUTEX_CLOCK_REALTIME takes a wall-clock deadline
// (musl's pthread_cond_timedwait).
func TestFutexRealtimeDeadline(t *testing.T) {
	r := newRig(t)
	p := r.spawn()
	addr := uint64(scratch + 64)
	r.put(p, addr, []byte{5, 0, 0, 0})
	deadline := time.Now().Add(30 * time.Millisecond)
	ts := make([]byte, 16)
	le.PutUint64(ts, uint64(deadline.Unix()))
	le.PutUint64(ts[8:], uint64(deadline.Nanosecond()))
	r.put(p, scratch+128, ts)
	start := time.Now()
	e := r.call(p, 202, addr, 9|128|256, 5, scratch+128, 0, 0xFFFFFFFF) // WAIT_BITSET|PRIVATE|CLOCK_REALTIME
	if el := time.Since(start); e != -int64(syscall.ETIMEDOUT) || el < 20*time.Millisecond || el > 2*time.Second {
		t.Errorf("realtime deadline: %d after %v", e, el)
	}
}
