package proc_test

import (
	"strings"
	"syscall"
	"testing"
	"time"

	"burrow/kernel/proc"
)

// musl's pthread_create flags.
const threadFlags = 0x100 | 0x200 | 0x400 | 0x800 | 0x10000 | 0x40000 | 0x80000 | 0x100000 | 0x200000

// spawnThread clones a thread of p with its own stack and TLS; it returns
// the new thread's TID and nucleus thread.
func (r *rig) spawnThread(p *proc.Process, stack, tls, ptid, ctid uint64) (int, uint64) {
	r.t.Helper()
	tid := r.call(p, 56, threadFlags, stack, ptid, ctid, tls)
	if tid <= 0 {
		r.t.Fatalf("clone = %d", tid)
	}
	return int(tid), r.lastThread(p.TID())
}

// lastThread is the nucleus thread started last after first.
func (r *rig) lastThread(first uint64) uint64 {
	last := uint64(0)
	for nt := first + 1; nt < first+100; nt++ {
		if r.nu.Thread(nt) != nil {
			last = nt
		}
	}
	if last == 0 {
		r.t.Fatal("no new thread")
	}
	return last
}

func TestCloneThread(t *testing.T) {
	r := newRig(t)
	p := r.spawn()
	r.running(p)
	stack := uint64(scratch + 0x800)
	tid, nt := r.spawnThread(p, stack, 0x7777000, scratch+16, scratch+24)
	if tid == p.PID {
		t.Fatal("the new thread has the pid as its tid")
	}
	th := r.nu.Snapshot(nt)
	if th.RSP != stack || th.Regs.RAX != 0 || th.FS != 0x7777000 || th.AS != p.Mem().Space() {
		t.Errorf("thread starts with rsp %#x rax %d fs %#x as %d", th.RSP, th.Regs.RAX, th.FS, th.AS)
	}
	b := make([]byte, 4)
	p.Mem().CopyIn(scratch+16, b)
	if int(le.Uint32(b)) != tid {
		t.Errorf("CLONE_PARENT_SETTID wrote %d", le.Uint32(b))
	}
	if got := r.callAs(nt, 186); got != int64(tid) { // gettid
		t.Errorf("gettid in the thread = %d, want %d", got, tid)
	}
	if got := r.callAs(nt, 39); got != int64(p.PID) { // getpid: one process
		t.Errorf("getpid in the thread = %d", got)
	}
	if e := r.call(p, 56, 0x100|0x10000, 0, 0, 0, 0); e != -int64(syscall.EINVAL) {
		t.Errorf("CLONE_THREAD without CLONE_SIGHAND = %d", e)
	}
}

// A thread exits alone; CLONE_CHILD_CLEARTID wakes whoever joins it; the
// last thread's exit ends the process.
func TestThreadExitAndJoin(t *testing.T) {
	r := newRig(t)
	p := r.spawn()
	ctid := uint64(scratch + 32)
	_, nt := r.spawnThread(p, scratch+0x800, 0, scratch+16, ctid)
	word := make([]byte, 4)
	p.Mem().CopyIn(ctid, word)
	val := le.Uint32(word) // the tid, as CLONE_CHILD_SETTID-alike: pthread_join waits while it is set
	r.put(p, ctid, word)
	r.nu.Inject(proc.Request{TID: p.TID(), Nr: 202, Args: [6]uint64{ctid, 0, uint64(val)}}) // join: FUTEX_WAIT
	time.Sleep(10 * time.Millisecond)
	r.nu.Inject(proc.Request{TID: nt, Nr: 60, Args: [6]uint64{0}}) // the thread exits
	if res := r.await(p.TID()); res != 0 {
		t.Fatalf("join woke with %d", res)
	}
	p.Mem().CopyIn(ctid, word)
	if le.Uint32(word) != 0 || !r.nu.Snapshot(nt).Killed {
		t.Errorf("after the thread's exit: clear_child_tid %d, killed %v", le.Uint32(word), r.nu.Snapshot(nt).Killed)
	}
	if pid := r.call(p, 39); pid != int64(p.PID) {
		t.Fatalf("the process went with its thread (getpid = %d)", pid)
	}
	_, nt2 := r.spawnThread(p, scratch+0x800, 0, 0, 0)
	r.nu.Inject(proc.Request{TID: nt2, Nr: 231, Args: [6]uint64{4}}) // exit_group from a thread
	if st, _ := wait(t, p); st != 4 || !r.nu.Snapshot(p.TID()).Killed && p.TID() != 0 {
		t.Errorf("exit_group: status %d", st)
	}
	r.waitFor(func() bool { return r.nu.FramesInUse() == 0 })
}

func TestFutex(t *testing.T) {
	r := newRig(t)
	p := r.spawn()
	_, nt := r.spawnThread(p, scratch+0x800, 0, 0, 0)
	addr := uint64(scratch + 64)
	r.put(p, addr, []byte{5, 0, 0, 0})
	if e := r.call(p, 202, addr, 128, 4); e != -int64(syscall.EAGAIN) { // FUTEX_WAIT_PRIVATE, wrong value
		t.Errorf("wait on a changed word = %d, want EAGAIN", e)
	}
	ts := make([]byte, 16)
	le.PutUint64(ts[8:], 20e6)
	r.put(p, scratch+128, ts)
	start := time.Now()
	if e := r.call(p, 202, addr, 128, 5, scratch+128); e != -int64(syscall.ETIMEDOUT) || time.Since(start) < 20*time.Millisecond {
		t.Errorf("timed wait = %d after %v", e, time.Since(start))
	}
	r.nu.Inject(proc.Request{TID: p.TID(), Nr: 202, Args: [6]uint64{addr, 128, 5}})
	time.Sleep(10 * time.Millisecond)
	if n := r.callAs(nt, 202, addr, 129, 1); n != 1 { // FUTEX_WAKE_PRIVATE
		t.Errorf("wake = %d, want 1", n)
	}
	if res := r.await(p.TID()); res != 0 {
		t.Errorf("woken wait = %d", res)
	}
	if n := r.call(p, 202, addr, 1, 10); n != 0 {
		t.Errorf("wake with nobody waiting = %d", n)
	}
	// REQUEUE: one waiter moves from addr to addr2 and is woken there
	addr2 := uint64(scratch + 96)
	r.nu.Inject(proc.Request{TID: p.TID(), Nr: 202, Args: [6]uint64{addr, 0, 5}})
	time.Sleep(10 * time.Millisecond)
	if n := r.callAs(nt, 202, addr, 3, 0, 1, addr2); n != 1 { // wake 0, requeue 1
		t.Errorf("requeue = %d", n)
	}
	if n := r.callAs(nt, 202, addr2, 1, 1); n != 1 {
		t.Errorf("wake on the requeued address = %d", n)
	}
	r.await(p.TID())
}

// tgkill reaches one thread; kill reaches a thread that does not block it.
func TestSignalsToThreads(t *testing.T) {
	r := newRig(t)
	p := r.spawn()
	r.install(p, sigUSR1, 0)
	tid, nt := r.spawnThread(p, scratch+0x800, 0, 0, 0)
	r.nu.SetRegs(nt, proc.FreshRegs(0x400250, scratch+0x800))
	r.running(p)
	if r.call(p, 234, uint64(p.PID), uint64(tid), sigUSR1) != 0 { // tgkill
		t.Fatal("tgkill")
	}
	r.waitFor(func() bool { return r.nu.Snapshot(nt).RIP == handlerVA })
	if r.nu.Snapshot(p.TID()).RIP == handlerVA {
		t.Error("the main thread ran the handler of a signal for the other one")
	}
	if e := r.call(p, 234, uint64(p.PID), 9999, sigUSR1); e != -int64(syscall.ESRCH) {
		t.Errorf("tgkill of no such thread = %d", e)
	}

	r2 := newRig(t)
	q := r2.spawn()
	r2.install(q, sigUSR1, 0)
	set := make([]byte, 8)
	le.PutUint64(set, 1<<(sigUSR1-1))
	r2.put(q, scratch+256, set)
	r2.call(q, 14, 0, scratch+256, 0, 8) // the main thread blocks SIGUSR1
	_, nt2 := r2.spawnThread(q, scratch+0x800, 0, 0, 0)
	if r2.callAs(nt2, 14, 1, scratch+256, 0, 8) != 0 { // the new thread inherited it: unblock there
		t.Fatal("sigprocmask in the thread")
	}
	r2.nu.SetRegs(nt2, proc.FreshRegs(0x400250, scratch+0x800))
	r2.running(q)
	r2.sys.Kill(int64(q.PID), sigUSR1)
	r2.waitFor(func() bool { return r2.nu.Snapshot(nt2).RIP == handlerVA })
}

// With SA_ONSTACK the frame goes on the thread's alternate stack.
func TestAltStack(t *testing.T) {
	r := newRig(t)
	p := r.spawn()
	r.install(p, sigUSR1, 0x08000000) // SA_ONSTACK
	alt := uint64(scratch + 0x1000)
	ss := make([]byte, 24)
	le.PutUint64(ss[0:], alt)
	le.PutUint64(ss[16:], 0x4000)
	r.put(p, scratch+512, ss)
	if r.call(p, 131, scratch+512, 0) != 0 {
		t.Fatal("sigaltstack")
	}
	old := make([]byte, 24)
	r.call(p, 131, 0, scratch+600)
	p.Mem().CopyIn(scratch+600, old)
	if le.Uint64(old) != alt || le.Uint64(old[16:]) != 0x4000 || le.Uint32(old[8:]) != 0 {
		t.Errorf("old stack %x", old)
	}
	ss[16] = 0x10 // 16 bytes: too small
	ss[17] = 0
	r.put(p, scratch+512, ss)
	if e := r.call(p, 131, scratch+512, 0); e != -int64(syscall.ENOMEM) {
		t.Errorf("tiny alternate stack = %d", e)
	}
	r.running(p)
	r.sys.Kill(int64(p.PID), sigUSR1)
	r.waitFor(func() bool { return r.nu.Snapshot(p.TID()).RIP == handlerVA })
	if sp := r.nu.Snapshot(p.TID()).RSP; sp < alt || sp >= alt+0x4000 {
		t.Errorf("handler rsp %#x is not on the alternate stack [%#x, %#x)", sp, alt, alt+0x4000)
	}
}

// A request a thread queued just before exit_group ended its process is
// dropped quietly (the Go runtime's other threads do this).
func TestLateRequestAfterExitGroup(t *testing.T) {
	r := newRig(t)
	p := r.spawn()
	_, nt := r.spawnThread(p, scratch+0x800, 0, 0, 0)
	r.nu.Inject(proc.Request{TID: p.TID(), Nr: 231})
	wait(t, p)
	r.nu.Inject(proc.Request{TID: nt, Nr: 281})
	time.Sleep(20 * time.Millisecond)
	if log := r.log.String(); strings.Contains(log, "unknown thread") {
		t.Errorf("logged a late request as unknown: %q", log)
	}
}
