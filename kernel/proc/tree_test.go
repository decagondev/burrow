package proc_test

import (
	"syscall"
	"testing"
	"time"

	"burrow/kernel/proc"
)

// await waits for one reply on thread tid (a blocked call answered later).
func (r *rig) await(tid uint64) int64 {
	r.t.Helper()
	select {
	case v := <-r.nu.Thread(tid).Replies:
		return v
	case <-time.After(2 * time.Second):
		r.t.Fatalf("thread %d got no reply", tid)
		return 0
	}
}

// callAs is call for a thread that is not p's (a fork child).
func (r *rig) callAs(tid uint64, nr uint64, args ...uint64) int64 {
	r.t.Helper()
	var a [6]uint64
	copy(a[:], args)
	r.nu.Inject(proc.Request{TID: tid, Nr: nr, Args: a})
	return r.await(tid)
}

// childTID finds the thread of a new process: the one started last.
func (r *rig) newestThread(before uint64) uint64 {
	for tid := before + 1; tid < before+100; tid++ {
		if r.nu.Thread(tid) != nil {
			return tid
		}
	}
	r.t.Fatal("no new thread")
	return 0
}

func TestForkCopiesMemoryAndRegisters(t *testing.T) {
	r := newRig(t)
	p := r.spawn()
	r.put(p, scratch, []byte("parent data"))
	regs := proc.FreshRegs(0x400250, scratch+4096)
	regs.RBX = 0x1234
	r.nu.SetRegs(p.TID(), regs)
	child := r.call(p, 57) // fork
	if child != int64(p.PID+1) {
		t.Fatalf("fork = %d", child)
	}
	kid := r.newestThread(p.TID())
	th := r.nu.Thread(kid)
	if th.AS == r.nu.Thread(p.TID()).AS || th.Regs.RAX != 0 || th.Regs.RBX != 0x1234 || th.RIP != 0x400250 {
		t.Errorf("child thread: as %d rax %d rbx %#x rip %#x", th.AS, th.Regs.RAX, th.Regs.RBX, th.RIP)
	}
	if ppid := r.callAs(kid, 110); ppid != int64(p.PID) {
		t.Errorf("child getppid = %d", ppid)
	}
	r.put(p, scratch, []byte("PARENT")) // the child's copy must not change
	r.nu.Inject(proc.Request{TID: kid, Nr: 1, Args: [6]uint64{1, scratch, 11}})
	if n := r.await(kid); n != 11 || r.console.String() != "parent data" {
		t.Errorf("child wrote %d bytes: %q", n, r.console.String())
	}
}

func TestWait4(t *testing.T) {
	r := newRig(t)
	p := r.spawn()
	if e := r.call(p, 61, ^uint64(0), 0, 1); e != -int64(syscall.ECHILD) {
		t.Errorf("wait4 with no children = %d", e)
	}
	child := r.call(p, 57)
	kid := r.newestThread(p.TID())
	if got := r.call(p, 61, ^uint64(0), scratch, 1); got != 0 { // WNOHANG, child running
		t.Errorf("wait4 WNOHANG = %d", got)
	}
	r.nu.Inject(proc.Request{TID: p.TID(), Nr: 61, Args: [6]uint64{uint64(child), scratch, 0}})
	time.Sleep(20 * time.Millisecond)                                 // the parent blocks
	r.nu.Inject(proc.Request{TID: kid, Nr: 231, Args: [6]uint64{42}}) // exit_group(42)
	if got := r.await(p.TID()); got != child {
		t.Fatalf("wait4 = %d, want %d", got, child)
	}
	b := make([]byte, 4)
	p.Mem().CopyIn(scratch, b)
	if st := le.Uint32(b); st != 42<<8 {
		t.Errorf("status %#x, want %#x (WEXITSTATUS 42)", st, 42<<8)
	}
	if e := r.call(p, 61, uint64(child), 0, 0); e != -int64(syscall.ECHILD) {
		t.Errorf("the child was not reaped: %d", e)
	}
}

func TestWait4KilledChildAndOrphans(t *testing.T) {
	r := newRig(t)
	p := r.spawn()
	child := r.call(p, 57)
	kid := r.newestThread(p.TID())
	grandchild := r.callAs(kid, 57)
	if grandchild <= child {
		t.Fatalf("grandchild pid %d", grandchild)
	}
	r.nu.Inject(proc.Request{TID: kid, Nr: proc.FaultBase | 14, Args: [6]uint64{0, 0x400000, 4}})
	if got := r.call(p, 61, ^uint64(0), scratch, 0); got != child {
		t.Fatalf("wait4 = %d", got)
	}
	b := make([]byte, 4)
	p.Mem().CopyIn(scratch, b)
	if st := le.Uint32(b); st != 11 {
		t.Errorf("status %#x, want 11 (WTERMSIG SIGSEGV)", st)
	}
	gk := r.newestThread(kid)
	if ppid := r.callAs(gk, 110); ppid != 1 {
		t.Errorf("orphan's ppid = %d, want 1 (init adopted it)", ppid)
	}
	r.nu.Inject(proc.Request{TID: gk, Nr: 231})
	r.waitFor(func() bool { // init reaps it: no zombie left
		for _, i := range r.sys.Processes() {
			if i.PID == int(grandchild) {
				return false
			}
		}
		return true
	})
}

func TestExecve(t *testing.T) {
	r := newRig(t)
	p := r.spawn()
	oldAS := r.nu.Thread(p.TID()).AS
	r.put(p, scratch, []byte("bin/prog\x00-x\x00"))
	argv := make([]byte, 24)
	le.PutUint64(argv, scratch)
	le.PutUint64(argv[8:], scratch+9)
	r.put(p, scratch+64, argv)
	if got := r.call(p, 59, scratch, scratch+64, 0); got != 0 {
		t.Fatalf("execve = %d", got)
	}
	th := r.nu.Thread(p.TID())
	if th.AS == oldAS || th.RIP != 0x400200 || th.Regs.RSP%16 != 0 || th.Regs.RFLAGS != 0x202 {
		t.Errorf("after execve: as %d rip %#x rsp %#x", th.AS, th.RIP, th.Regs.RSP)
	}
	argc := make([]byte, 8)
	p.Mem().CopyIn(th.Regs.RSP, argc)
	if le.Uint64(argc) != 2 {
		t.Errorf("argc %d", le.Uint64(argc))
	}
	r.put(p, scratch, []byte("etc/motd\x00"))
	if e := r.call(p, 59, scratch, 0, 0); e != -int64(syscall.ENOEXEC) {
		t.Errorf("execve of a text file = %d", e)
	}
	r.put(p, scratch, []byte("nope\x00"))
	if e := r.call(p, 59, scratch, 0, 0); e != -int64(syscall.ENOENT) {
		t.Errorf("execve of a missing file = %d", e)
	}
}
