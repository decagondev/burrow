package proc

// signal.go - delivering signals (W10-S04, threads since W12-S01). A
// process-directed signal (kill) is pending on the process and taken by the
// first thread that does not block it; a thread-directed one (tgkill) is
// pending on that thread. A thread takes a signal when a request of its
// finishes or when an interrupt stop arrives, and runs the handler on its
// own stack or, with SA_ONSTACK, its alternate stack. The system calls are
// in sigcalls.go. Docs: POSIX_COVERAGE.md (Signals).

import (
	"fmt"
	"sort"
	"syscall"
)

// Signals (Linux x86-64 numbers).
const (
	sigHUP   = 1
	sigINT   = 2
	sigQUIT  = 3
	sigTRAP  = 5
	sigABRT  = 6
	sigBUS   = 7
	sigKILL  = 9
	sigPIPE  = 13
	sigTERM  = 15
	sigCONT  = 18
	sigSTOP  = 19
	sigTSTP  = 20
	sigTTIN  = 21
	sigTTOU  = 22
	sigURG   = 23
	sigWINCH = 28
	nSig     = 64

	sigDFL = 0
	sigIGN = 1

	saSiginfo   = 4
	saOnstack   = 0x08000000
	saRestorer  = 0x04000000
	saRestart   = 0x10000000
	saNodefer   = 0x40000000
	saResethand = 0x80000000

	siUser   = 0 // si_code: sent by kill
	siTkill  = -6
	siKernel = 0x80

	unblockable = 1<<(sigKILL-1) | 1<<(sigSTOP-1)
)

// sigaction is struct k_sigaction (32 bytes: handler, flags, restorer, mask).
type sigaction struct{ handler, flags, restorer, mask uint64 }

func bit(sig int) uint64 { return 1 << (sig - 1) }

// ignoredByDefault: SIG_DFL means "ignore" (job control stops count as
// ignore until stopped processes exist).
func ignoredByDefault(sig int) bool {
	switch sig {
	case sigCHLD, sigCONT, sigURG, sigWINCH, sigSTOP, sigTSTP, sigTTIN, sigTTOU:
		return true
	}
	return false
}

func sigName(sig int) string {
	names := map[int]string{sigHUP: "SIGHUP", sigINT: "SIGINT", sigQUIT: "SIGQUIT", sigILL: "SIGILL",
		sigABRT: "SIGABRT", sigFPE: "SIGFPE", sigKILL: "SIGKILL", sigSEGV: "SIGSEGV", sigPIPE: "SIGPIPE",
		sigTERM: "SIGTERM"}
	if n, ok := names[sig]; ok {
		return n
	}
	return fmt.Sprintf("signal %d", sig)
}

// discarded: sig would be dropped on arrival (p.mu held).
func (p *Process) discarded(sig int) bool {
	act := p.actions[sig]
	return sig != sigKILL && (act.handler == sigIGN || act.handler == sigDFL && ignoredByDefault(sig))
}

// send makes sig pending for p and wakes the first thread (by TID) that
// does not block it: its blocking call returns EINTR, or, computing in
// ring 3, it stops at its next tick (Interrupt).
func (p *Process) send(sig int, info sigInfo) {
	p.mu.Lock()
	if p.exited || sig < 1 || sig > nSig || len(p.threads) == 0 || p.discarded(sig) {
		p.mu.Unlock() // init (no threads) takes no signals
		return
	}
	p.pending |= bit(sig)
	p.info[sig] = info
	tids := make([]int, 0, len(p.threads))
	for tid := range p.threads {
		tids = append(tids, tid)
	}
	sort.Ints(tids)
	var target *Thread
	for _, tid := range tids {
		if t := p.threads[tid]; !t.exited && (t.sigMask&bit(sig) == 0 || sig == sigKILL) {
			target = t
			break
		}
	}
	if target != nil {
		target.wakeLocked()
	}
	p.mu.Unlock()
	if target != nil {
		p.sys.nu.Interrupt(target.nt)
	}
}

// send makes sig pending for t alone (tgkill).
func (t *Thread) send(sig int, info sigInfo) {
	p := t.p
	p.mu.Lock()
	if p.exited || t.exited || sig < 1 || sig > nSig || p.discarded(sig) {
		p.mu.Unlock()
		return
	}
	t.pending |= bit(sig)
	t.info[sig] = info
	deliverable := t.sigMask&bit(sig) == 0 || sig == sigKILL
	if deliverable {
		t.wakeLocked()
	}
	p.mu.Unlock()
	if deliverable {
		p.sys.nu.Interrupt(t.nt)
	}
}

// deliverableLocked: a signal t could take now (p.mu held).
func (t *Thread) deliverableLocked() bool {
	return (t.pending|t.p.pending)&^(t.sigMask&^unblockable) != 0
}

// takeSignal removes the lowest signal t can take, its own first (0 if none).
func (t *Thread) takeSignal() (int, sigaction, sigInfo) {
	p := t.p
	p.mu.Lock()
	defer p.mu.Unlock()
	ready := (t.pending | p.pending) &^ (t.sigMask &^ unblockable)
	if ready == 0 {
		select { // nothing left: blocking calls may block again
		case <-t.sigWake:
			t.sigWake = make(chan struct{})
		default:
		}
		return 0, sigaction{}, sigInfo{}
	}
	sig := 1
	for ready&bit(sig) == 0 {
		sig++
	}
	info := p.info[sig]
	if t.pending&bit(sig) != 0 {
		t.pending &^= bit(sig)
		info = t.info[sig]
	} else {
		p.pending &^= bit(sig)
	}
	return sig, p.actions[sig], info
}

// neverRestarted are the calls Linux fails with EINTR after a handler even
// with SA_RESTART (signal(7)): poll, ppoll, pause, nanosleep, rt_sigsuspend,
// epoll_wait, epoll_pwait. (FUTEX_WAIT is restarted.)
var neverRestarted = map[uint64]bool{7: true, 271: true, 34: true, 35: true, 130: true, 232: true, 281: true}

// finish answers a system call: before the thread returns to ring 3, a
// pending signal runs its handler (or its default action) first.
func (t *Thread) finish(res int64, nr uint64) {
	p, s := t.p, t.p.sys
	p.mu.Lock()
	gone := t.exited
	p.mu.Unlock()
	if gone { // its process ended while the call waited: the nucleus thread is
		return // gone, a reply would only fail (BUG-0026; ids are never reused)
	}
	sig, act, info := t.takeSignal()
	if sig == 0 {
		p.mu.Lock()
		if t.suspended { // nothing was delivered after all: put the mask back
			t.sigMask, t.suspended = t.suspendMask, false
		}
		p.mu.Unlock()
		s.nu.Reply(t.nt, res)
		return
	}
	var r Regs
	if err := s.nu.Regs(t.nt, &r, false); err != nil {
		s.nu.Reply(t.nt, res)
		return
	}
	r.RAX = uint64(res)
	if res == -int64(syscall.EINTR) && act.flags&saRestart != 0 && !neverRestarted[nr] {
		r.RIP -= 2 // back onto the SYSCALL instruction: the call runs again
		r.RAX = nr
	}
	if t.deliver(sig, act, info, &r) {
		s.nu.Reply(t.nt, int64(r.RAX))
	}
}

// deliver runs sig's handler from registers r (written back to the
// thread), or its default action. false: the process is gone.
func (t *Thread) deliver(sig int, act sigaction, info sigInfo, r *Regs) bool {
	p := t.p
	switch {
	case act.handler == sigIGN || act.handler == sigDFL && ignoredByDefault(sig):
		return t.resume(r)
	case act.handler == sigDFL || sig == sigKILL:
		p.exit(0, sig, "")
		return false
	}
	p.mu.Lock()
	old := t.sigMask
	if t.suspended { // rt_sigsuspend: the handler returns to the mask before the call
		old, t.suspended = t.suspendMask, false
	}
	if act.flags&saNodefer == 0 {
		t.sigMask |= bit(sig)
	}
	t.sigMask |= act.mask &^ unblockable
	if act.flags&saResethand != 0 {
		p.actions[sig] = sigaction{}
	}
	alt := altStack{t.altSP, t.altSize}
	p.mu.Unlock()
	h, err := p.pushFrame(*r, sig, act, old, info, alt)
	if err != nil { // no room on the stack: as Linux, the process dies
		p.exit(0, sigSEGV, "no stack for a signal frame")
		return false
	}
	*r = h
	return t.resume(r)
}

func (t *Thread) resume(r *Regs) bool {
	if err := t.p.sys.nu.Regs(t.nt, r, true); err != nil {
		t.p.sys.log.Errorf("pid %d tid %d: registers: %v", t.p.PID, t.TID, err)
	}
	return true
}

// stopped answers an interrupt or fault stop: deliver what is pending.
func (t *Thread) stopped(fault bool, sig int, info sigInfo, rip uint64) {
	p, s := t.p, t.p.sys
	if fault {
		p.mu.Lock()
		act, blocked := p.actions[sig], t.sigMask&bit(sig) != 0
		p.mu.Unlock()
		if act.handler == sigDFL || act.handler == sigIGN || blocked { // a fault cannot be ignored
			p.exit(0, sig, fmt.Sprintf("%s at %#x (rip %#x)", info.what, info.addr, rip))
			return
		}
		var r Regs
		if s.nu.Regs(t.nt, &r, false) == nil && t.deliver(sig, act, info, &r) {
			s.nu.Reply(t.nt, 0)
		}
		return
	}
	sig, act, si := t.takeSignal()
	if sig != 0 {
		var r Regs
		if s.nu.Regs(t.nt, &r, false) != nil || !t.deliver(sig, act, si, &r) {
			return
		}
	}
	s.nu.Reply(t.nt, 0)
}
