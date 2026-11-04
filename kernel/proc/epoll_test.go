package proc_test

import (
	"syscall"
	"testing"
	"time"

	"burrow/kernel/proc"
)

// counter writes v to an eventfd (or anything 8 bytes long).
func (r *rig) put64(p *proc.Process, fd int64, v uint64) int64 {
	b := make([]byte, 8)
	le.PutUint64(b, v)
	r.put(p, scratch+1024, b)
	return r.call(p, 1, uint64(fd), scratch+1024, 8)
}

func (r *rig) get64(p *proc.Process, fd int64) (uint64, int64) {
	n := r.call(p, 0, uint64(fd), scratch+1024, 8)
	b := make([]byte, 8)
	p.Mem().CopyIn(scratch+1024, b)
	return le.Uint64(b), n
}

func TestEventfd(t *testing.T) {
	r := newRig(t)
	p := r.spawn()
	efd := r.call(p, 290, 3, 0x800) // initval 3, EFD_NONBLOCK
	if v, n := r.get64(p, efd); n != 8 || v != 3 {
		t.Errorf("read = %d bytes, %d", n, v)
	}
	if _, n := r.get64(p, efd); n != -int64(syscall.EAGAIN) {
		t.Errorf("read of 0 non-blocking = %d", n)
	}
	r.put64(p, efd, 2)
	r.put64(p, efd, 5)
	if v, _ := r.get64(p, efd); v != 7 {
		t.Errorf("counter = %d, want 7", v)
	}
	sem := r.call(p, 290, 2, 1) // EFD_SEMAPHORE, blocking
	for i := 0; i < 2; i++ {
		if v, _ := r.get64(p, sem); v != 1 {
			t.Errorf("semaphore read %d = %d", i, v)
		}
	}
	r.nu.Inject(proc.Request{TID: p.TID(), Nr: 0, Args: [6]uint64{uint64(sem), scratch + 1100, 8}})
	time.Sleep(10 * time.Millisecond) // blocked at 0
	_, nt := r.spawnThread(p, scratch+0x800, 0, 0, 0)
	b := make([]byte, 8)
	le.PutUint64(b, 1)
	r.put(p, scratch+1200, b)
	r.callAs(nt, 1, uint64(sem), scratch+1200, 8)
	if n := r.await(p.TID()); n != 8 {
		t.Errorf("blocked read woke with %d", n)
	}
	if e := r.call(p, 290, 0, 0x4); e != -int64(syscall.EINVAL) {
		t.Errorf("bad flags = %d", e)
	}
}

// epollEvents reads n epoll_events at va: (events, data) pairs.
func epollEvents(p *proc.Process, va uint64, n int64) [][2]uint64 {
	b := make([]byte, 12*n)
	p.Mem().CopyIn(va, b)
	var out [][2]uint64
	for i := int64(0); i < n; i++ {
		out = append(out, [2]uint64{uint64(le.Uint32(b[12*i:])), le.Uint64(b[12*i+4:])})
	}
	return out
}

func (r *rig) epollCtl(p *proc.Process, ep, op, fd int64, events uint32, data uint64) int64 {
	b := make([]byte, 12)
	le.PutUint32(b, events)
	le.PutUint64(b[4:], data)
	r.put(p, scratch+1300, b)
	return r.call(p, 233, uint64(ep), uint64(op), uint64(fd), scratch+1300)
}

func TestEpoll(t *testing.T) {
	r := newRig(t)
	p := r.spawn()
	ep := r.call(p, 291, 0)
	r.call(p, 22, scratch) // pipe 4 (read), 5 (write) after the epoll fd 3
	efd := r.call(p, 290, 0, 0)
	evs := uint64(scratch + 1400)
	if r.epollCtl(p, ep, 1, 4, 0x1, 44) != 0 || r.epollCtl(p, ep, 1, efd, 0x1|1<<31, 66) != 0 { // pipe LT, eventfd ET
		t.Fatal("epoll_ctl ADD")
	}
	if e := r.epollCtl(p, ep, 1, 4, 0x1, 44); e != -int64(syscall.EEXIST) {
		t.Errorf("second ADD = %d", e)
	}
	r.str(p, scratch+64, "/etc/motd")
	file := r.call(p, 2, scratch+64, 0)
	if e := r.epollCtl(p, ep, 1, file, 0x1, 0); e != -int64(syscall.EPERM) {
		t.Errorf("ADD of a regular file = %d, want EPERM", e)
	}
	start := time.Now()
	if n := r.call(p, 232, uint64(ep), evs, 8, 20); n != 0 || time.Since(start) < 20*time.Millisecond {
		t.Errorf("wait with nothing ready = %d after %v", n, time.Since(start))
	}
	r.call(p, 1, 5, scratch, 3) // data in the pipe
	r.put64(p, efd, 1)
	n := r.call(p, 232, uint64(ep), evs, 8, 0)
	if got := epollEvents(p, evs, n); n != 2 || got[0] != [2]uint64{1, 44} || got[1] != [2]uint64{1, 66} {
		t.Fatalf("wait = %d: %v", n, got)
	}
	n = r.call(p, 232, uint64(ep), evs, 8, 0) // level: the pipe again; edge: not the eventfd
	if got := epollEvents(p, evs, n); n != 1 || got[0][1] != 44 {
		t.Errorf("second wait = %d: %v (want only the level-triggered pipe)", n, got)
	}
	r.put64(p, efd, 1) // something happened: the edge fires again
	n = r.call(p, 232, uint64(ep), evs, 8, 0)
	if n != 2 {
		t.Errorf("after a new write: %d events, want 2", n)
	}
	if r.epollCtl(p, ep, 2, 4, 0, 0) != 0 || r.epollCtl(p, ep, 3, efd, 0x1|1<<30, 77) != 0 { // DEL pipe; eventfd ONESHOT
		t.Fatal("epoll_ctl DEL/MOD")
	}
	if n := r.call(p, 232, uint64(ep), evs, 8, 0); n != 1 || epollEvents(p, evs, 1)[0][1] != 77 {
		t.Errorf("oneshot first = %d", n)
	}
	if n := r.call(p, 232, uint64(ep), evs, 8, 0); n != 0 {
		t.Errorf("oneshot fired twice: %d", n)
	}
	// a blocked wait wakes when another thread writes
	r.epollCtl(p, ep, 1, 4, 0x1, 44)
	r.get64(p, 4) // drain the pipe (3 bytes; read up to 8)
	_, nt := r.spawnThread(p, scratch+0x800, 0, 0, 0)
	r.nu.Inject(proc.Request{TID: p.TID(), Nr: 281, Args: [6]uint64{uint64(ep), evs, 8, ^uint64(0)}})
	time.Sleep(10 * time.Millisecond)
	r.callAs(nt, 1, 5, scratch, 1)
	if n := r.await(p.TID()); n != 1 {
		t.Errorf("blocked epoll_pwait woke with %d", n)
	}
}

// TestBUG0022EpollAddWakesBlockedWait: Go's netpoller sits in epoll_wait
// while another thread connects and ADDs the new socket; the wait must see
// the new item, or the connection's readiness waits for the poller's timer.
func TestBUG0022EpollAddWakesBlockedWait(t *testing.T) {
	r := newRig(t)
	p := r.spawn()
	ep := r.call(p, 291, 0)
	idle := r.call(p, 290, 0, 0x800) // never ready
	if r.epollCtl(p, ep, 1, idle, 0x1|1<<31, 1) != 0 {
		t.Fatal("epoll_ctl ADD")
	}
	ready := r.call(p, 290, 1, 0x800) // readable from the start
	evs := uint64(scratch + 1400)
	_, nt := r.spawnThread(p, scratch+0x800, 0, 0, 0)
	r.nu.Inject(proc.Request{TID: p.TID(), Nr: 232, Args: [6]uint64{uint64(ep), evs, 8, 2000}})
	time.Sleep(10 * time.Millisecond) // blocked, waiting only on idle
	start := time.Now()
	b := make([]byte, 12)
	le.PutUint32(b, 0x1|1<<31)
	le.PutUint64(b[4:], 2)
	r.put(p, scratch+1300, b)
	if e := r.callAs(nt, 233, uint64(ep), 1, uint64(ready), scratch+1300); e != 0 {
		t.Fatalf("ADD from another thread = %d", e)
	}
	n := r.await(p.TID())
	if n != 1 || epollEvents(p, evs, 1)[0] != [2]uint64{1, 2} || time.Since(start) > time.Second {
		t.Errorf("wait after ADD = %d after %v, want the new item at once", n, time.Since(start))
	}
}

func TestMadviseAndLimits(t *testing.T) {
	r := newRig(t)
	p := r.spawn()
	addr := uint64(r.call(p, 9, 0, 8192, 3, 0x22, ^uint64(0), 0))
	r.put(p, addr+100, []byte("data"))
	if r.call(p, 28, addr, 8192, 4) != 0 { // MADV_DONTNEED
		t.Fatal("madvise")
	}
	b := make([]byte, 4)
	p.Mem().CopyIn(addr+100, b)
	if string(b) != "\x00\x00\x00\x00" {
		t.Errorf("after MADV_DONTNEED: %q", b)
	}
	if r.call(p, 302, 0, 7, 0, scratch+64) != 0 { // prlimit64(RLIMIT_NOFILE)
		t.Fatal("prlimit64")
	}
	lim := make([]byte, 16)
	p.Mem().CopyIn(scratch+64, lim)
	if le.Uint64(lim) != 256 || le.Uint64(lim[8:]) != 256 {
		t.Errorf("RLIMIT_NOFILE %d/%d", le.Uint64(lim), le.Uint64(lim[8:]))
	}
	if r.call(p, 302, 0, 7, scratch+64, 0) != 0 { // setting it to what it is
		t.Error("prlimit64 set to the hard limit")
	}
	le.PutUint64(lim[8:], 1<<20)
	r.put(p, scratch+64, lim)
	if e := r.call(p, 302, 0, 7, scratch+64, 0); e != -int64(syscall.EPERM) {
		t.Errorf("raising the hard limit = %d", e)
	}
}
