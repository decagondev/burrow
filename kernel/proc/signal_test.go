package proc_test

import (
	"syscall"
	"testing"
	"time"

	"burrow/kernel/proc"
)

const (
	sigINT, sigUSR1, sigTERM, sigSEGV = 2, 10, 15, 11
	handlerVA, restorerVA             = 0x400300, 0x400310
	saRestorer, saRestart             = 0x04000000, 0x10000000
)

// install sets a handler for sig with musl's flags (SA_RESTORER [+ more]).
func (r *rig) install(p *proc.Process, sig int, flags uint64) {
	act := make([]byte, 32)
	le.PutUint64(act, handlerVA)
	le.PutUint64(act[8:], saRestorer|flags)
	le.PutUint64(act[16:], restorerVA)
	r.put(p, scratch+2048, act)
	if r.call(p, 13, uint64(sig), scratch+2048, 0, 8) != 0 {
		r.t.Fatal("rt_sigaction")
	}
}

// running gives p's thread a recognisable state, as if computing.
func (r *rig) running(p *proc.Process) proc.Regs {
	regs := proc.FreshRegs(0x400250, proc.StackTop-0x1000)
	regs.RBX, regs.RAX = 7, 99
	regs.FX[160] = 0xAB // inside XMM0
	r.nu.SetRegs(p.TID(), regs)
	return regs
}

func (r *rig) waitFor(cond func() bool) {
	r.t.Helper()
	for deadline := time.Now().Add(2 * time.Second); !cond(); time.Sleep(time.Millisecond) {
		if time.Now().After(deadline) {
			r.t.Fatal("timed out")
		}
	}
}

func TestHandlerRunsAndSigreturnRestores(t *testing.T) {
	r := newRig(t)
	p := r.spawn()
	r.install(p, sigUSR1, 0)
	before := r.running(p)
	if err := r.sys.Kill(int64(p.PID), sigUSR1); err != nil {
		t.Fatal(err)
	}
	r.waitFor(func() bool { return r.nu.Snapshot(p.TID()).RIP == handlerVA })
	h := r.nu.Snapshot(p.TID()).Regs
	if h.RDI != sigUSR1 || h.RSP%16 != 8 || h.RSP >= before.RSP-128 {
		t.Errorf("handler entry: rdi %d rsp %#x", h.RDI, h.RSP)
	}
	ret := make([]byte, 8)
	p.Mem().CopyIn(h.RSP, ret)
	if le.Uint64(ret) != restorerVA {
		t.Errorf("return address %#x, want the restorer", le.Uint64(ret))
	}
	info := make([]byte, 4)
	p.Mem().CopyIn(h.RSI, info)
	if le.Uint32(info) != sigUSR1 {
		t.Errorf("siginfo.si_signo %d", le.Uint32(info))
	}
	// the handler returns: ret pops pretcode, the restorer calls rt_sigreturn
	h.RIP, h.RSP = restorerVA, h.RSP+8
	h.RBX, h.FX[160] = 0, 0 // the handler clobbered them
	r.nu.SetRegs(p.TID(), h)
	if got := r.call(p, 15); got != 99 {
		t.Errorf("rt_sigreturn = %d, want the interrupted RAX 99", got)
	}
	back := r.nu.Snapshot(p.TID()).Regs
	if back.RIP != before.RIP || back.RSP != before.RSP || back.RBX != 7 || back.FX[160] != 0xAB {
		t.Errorf("not restored: rip %#x rsp %#x rbx %d xmm0 %#x", back.RIP, back.RSP, back.RBX, back.FX[160])
	}
}

func TestDefaultActionsAndIgnore(t *testing.T) {
	r := newRig(t)
	p := r.spawn()
	r.running(p)
	r.call(p, 13, sigINT, r.sigIgn(p), 0, 8)
	r.sys.Kill(int64(p.PID), sigINT)
	if n := r.nu.Snapshot(p.TID()).Interrupts; n != 0 {
		t.Errorf("an ignored signal stopped the thread %d times", n)
	}
	r.sys.Kill(int64(p.PID), sigTERM)
	if st, _ := wait(t, p); st != 128+sigTERM {
		t.Errorf("SIGTERM: status %d", st)
	}
	if err := r.sys.Kill(int64(p.PID), sigTERM); err != syscall.ESRCH {
		t.Errorf("kill of a dead process = %v", err)
	}
}

func (r *rig) sigIgn(p *proc.Process) uint64 {
	act := make([]byte, 32)
	le.PutUint64(act, 1) // SIG_IGN
	r.put(p, scratch+2048, act)
	return scratch + 2048
}

func TestBlockingCallInterrupted(t *testing.T) {
	for _, restart := range []bool{false, true} {
		r := newRig(t)
		p := r.spawn()
		flags := uint64(0)
		if restart {
			flags = saRestart
		}
		r.install(p, sigINT, flags)
		r.call(p, 22, scratch) // pipe: 3 and 4
		regs := r.running(p)
		regs.RIP = 0x400262 // just after its SYSCALL
		r.nu.SetRegs(p.TID(), regs)
		r.nu.Inject(proc.Request{TID: p.TID(), Nr: 0, Args: [6]uint64{3, scratch + 64, 10}})
		time.Sleep(10 * time.Millisecond) // blocked in read
		r.sys.Kill(int64(p.PID), sigINT)
		got := r.await(p.TID())
		h := r.nu.Snapshot(p.TID()).Regs
		if h.RIP != handlerVA {
			t.Fatalf("restart=%v: rip %#x after the signal", restart, h.RIP)
		}
		uc := make([]byte, 304)
		p.Mem().CopyIn(h.RDX, uc)
		savedRIP, savedRAX := le.Uint64(uc[40+16*8:]), le.Uint64(uc[40+13*8:])
		switch {
		case !restart && (int64(savedRAX) != -int64(syscall.EINTR) || savedRIP != 0x400262):
			t.Errorf("no SA_RESTART: saved rax %#x rip %#x, want -EINTR at 0x400262", savedRAX, savedRIP)
		case restart && (savedRAX != 0 || savedRIP != 0x400260):
			t.Errorf("SA_RESTART: saved rax %d rip %#x, want read (0) again at 0x400260", savedRAX, savedRIP)
		}
		_ = got
	}
}

func TestMaskedSignalWaitsForUnblock(t *testing.T) {
	r := newRig(t)
	p := r.spawn()
	r.install(p, sigUSR1, 0)
	set := make([]byte, 8)
	le.PutUint64(set, 1<<(sigUSR1-1))
	r.put(p, scratch, set)
	r.call(p, 14, 0, scratch, 0, 8) // SIG_BLOCK
	r.running(p)
	r.sys.Kill(int64(p.PID), sigUSR1)
	if r.call(p, 39) != int64(p.PID) || r.nu.Snapshot(p.TID()).RIP == handlerVA {
		t.Fatal("a blocked signal was delivered")
	}
	r.call(p, 14, 1, scratch, scratch+8, 8) // SIG_UNBLOCK: delivered on this call's return
	if r.nu.Snapshot(p.TID()).RIP != handlerVA {
		t.Error("unblocking did not deliver the pending signal")
	}
	old := make([]byte, 8)
	p.Mem().CopyIn(scratch+8, old)
	if le.Uint64(old) != 1<<(sigUSR1-1) {
		t.Errorf("old mask %#x", le.Uint64(old))
	}
}

func TestFaultGoesToHandler(t *testing.T) {
	r := newRig(t)
	p := r.spawn()
	r.install(p, sigSEGV, 0)
	r.running(p)
	r.nu.Inject(proc.Request{TID: p.TID(), Nr: proc.FaultBase | 14, Args: [6]uint64{0x1234, 0x400250, 4}})
	r.await(p.TID())
	h := r.nu.Snapshot(p.TID()).Regs
	addr := make([]byte, 8)
	p.Mem().CopyIn(h.RSI+16, addr)
	if h.RIP != handlerVA || h.RDI != sigSEGV || le.Uint64(addr) != 0x1234 {
		t.Errorf("SIGSEGV handler: rip %#x sig %d si_addr %#x", h.RIP, h.RDI, le.Uint64(addr))
	}
}
