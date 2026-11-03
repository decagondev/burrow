package proc_test

import (
	"syscall"
	"testing"
	"time"

	"burrow/kernel/proc"
)

// fdset stages one fd_set (16 bytes: fds 0-127) at va with the given fds.
func (r *rig) fdset(p *proc.Process, va uint64, fds ...int) {
	b := make([]byte, 16)
	for _, fd := range fds {
		b[fd/8] |= 1 << (fd % 8)
	}
	r.put(p, va, b)
}

func isSet(p *proc.Process, va uint64, fd int) bool {
	b := make([]byte, 1)
	p.Mem().CopyIn(va+uint64(fd/8), b)
	return b[0]>>(fd%8)&1 != 0
}

func TestSelectPipe(t *testing.T) {
	r := newRig(t)
	p := r.spawn()
	r.call(p, 22, scratch) // pipe: 3 reads, 4 writes
	rd, wr := uint64(scratch+256), uint64(scratch+272)
	zero := uint64(scratch + 288)
	r.put(p, zero, make([]byte, 16)) // struct timeval {0, 0}: do not wait
	r.fdset(p, rd, 3)
	r.fdset(p, wr, 4)
	if n := r.call(p, 23, 5, rd, wr, 0, zero); n != 1 || isSet(p, rd, 3) || !isSet(p, wr, 4) {
		t.Errorf("empty pipe: %d, read set %v, write set %v", n, isSet(p, rd, 3), isSet(p, wr, 4))
	}
	r.call(p, 1, 4, scratch, 2)
	r.fdset(p, rd, 3)
	r.fdset(p, wr, 4)
	if n := r.call(p, 23, 5, rd, wr, 0, 0); n != 2 || !isSet(p, rd, 3) || !isSet(p, wr, 4) {
		t.Errorf("with data: %d", n)
	}
	r.fdset(p, rd, 3, 9)
	if e := r.call(p, 23, 10, rd, 0, 0, zero); e != -int64(syscall.EBADF) {
		t.Errorf("select on a closed fd = %d, want EBADF", e)
	}
	r.fdset(p, rd, 3, 9) // fd 9 lies beyond nfds: never looked at
	if n := r.call(p, 23, 4, rd, 0, 0, zero); n != 1 {
		t.Errorf("nfds 4 = %d, want 1", n)
	}
}

func TestSelectTimeouts(t *testing.T) {
	r := newRig(t)
	p := r.spawn()
	r.call(p, 22, scratch)
	rd, tv := uint64(scratch+256), uint64(scratch+288)
	b := make([]byte, 16)
	le.PutUint64(b[8:], 30000) // 30 ms in microseconds
	r.put(p, tv, b)
	r.fdset(p, rd, 3)
	start := time.Now()
	if n := r.call(p, 23, 4, rd, 0, 0, tv); n != 0 || isSet(p, rd, 3) || time.Since(start) < 30*time.Millisecond {
		t.Errorf("select timeout: %d after %v, still set %v", n, time.Since(start), isSet(p, rd, 3))
	}
	le.PutUint64(b[8:], 20e6) // pselect6: 20 ms in nanoseconds
	r.put(p, tv, b)
	r.fdset(p, rd, 3)
	start = time.Now()
	if n := r.call(p, 270, 4, rd, 0, 0, tv, 0); n != 0 || time.Since(start) < 20*time.Millisecond {
		t.Errorf("pselect6 timeout: %d after %v", n, time.Since(start))
	}
	if e := r.call(p, 23, 0, 0, 0, 0, tv); e != -int64(syscall.EINVAL) { // 20000000 us: out of range
		t.Errorf("select with usec out of range = %d, want EINVAL", e)
	}
	le.PutUint64(b[8:], 10000)
	r.put(p, tv, b)
	start = time.Now()
	if n := r.call(p, 23, 0, 0, 0, 0, tv); n != 0 || time.Since(start) < 10*time.Millisecond {
		t.Errorf("select as a sleep: %d after %v", n, time.Since(start))
	}
}

// A select blocked on an empty pipe wakes when another process writes.
func TestSelectWakes(t *testing.T) {
	r := newRig(t)
	p := r.spawn()
	r.call(p, 22, scratch)
	rd := uint64(scratch + 256)
	r.fdset(p, rd, 3)
	r.call(p, 57) // fork: the child shares the pipe
	kid := r.newestThread(p.TID())
	r.nu.Inject(proc.Request{TID: p.TID(), Nr: 23, Args: [6]uint64{4, rd, 0, 0, 0}})
	time.Sleep(20 * time.Millisecond) // blocks: nothing written yet
	if n := r.callAs(kid, 1, 4, scratch, 1); n != 1 {
		t.Fatalf("child write = %d", n)
	}
	if n := r.await(p.TID()); n != 1 || !isSet(p, rd, 3) {
		t.Errorf("woken select = %d", n)
	}
}
