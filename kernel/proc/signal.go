package proc

import (
	"encoding/binary"
	"fmt"
	"syscall"
	"time"
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
	saRestorer  = 0x04000000
	saRestart   = 0x10000000
	saNodefer   = 0x40000000
	saResethand = 0x80000000

	siUser   = 0 // si_code: sent by kill
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

// send makes sig pending for p and wakes it: blocking calls return EINTR,
// and a thread computing in ring 3 stops at its next tick (Interrupt).
func (p *Process) send(sig int, info sigInfo) {
	p.mu.Lock()
	if p.exited || sig < 1 || sig > nSig {
		p.mu.Unlock()
		return
	}
	act := p.actions[sig]
	if sig != sigKILL && (act.handler == sigIGN || act.handler == sigDFL && ignoredByDefault(sig)) {
		p.mu.Unlock()
		return // discarded, never pending
	}
	p.pending |= bit(sig)
	p.info[sig] = info
	deliverable := p.pending&^p.sigMask != 0 || sig == sigKILL
	if deliverable {
		p.wakeLocked()
	}
	p.mu.Unlock()
	if deliverable {
		p.sys.nu.Interrupt(p.tid)
	}
}

// wakeLocked closes sigWake once (p.mu held); takeSignal makes a new one.
func (p *Process) wakeLocked() {
	select {
	case <-p.sigWake:
	default:
		close(p.sigWake)
	}
}

// takeSignal removes the lowest deliverable pending signal (0 if none).
func (p *Process) takeSignal() (int, sigaction, sigInfo) {
	p.mu.Lock()
	defer p.mu.Unlock()
	ready := p.pending &^ (p.sigMask &^ unblockable)
	if ready == 0 {
		select { // nothing left: blocking calls may block again
		case <-p.sigWake:
			p.sigWake = make(chan struct{})
		default:
		}
		return 0, sigaction{}, sigInfo{}
	}
	sig := 1
	for ready&bit(sig) == 0 {
		sig++
	}
	p.pending &^= bit(sig)
	return sig, p.actions[sig], p.info[sig]
}

// finish answers a system call: before the thread returns to ring 3, a
// pending signal runs its handler (or its default action) first.
func (p *Process) finish(res int64, nr uint64) {
	s := p.sys
	sig, act, info := p.takeSignal()
	if sig == 0 {
		p.mu.Lock()
		if p.suspended { // nothing was delivered after all: put the mask back
			p.sigMask, p.suspended = p.suspendMask, false
		}
		p.mu.Unlock()
		s.nu.Reply(p.tid, res)
		return
	}
	var r Regs
	if err := s.nu.Regs(p.tid, &r, false); err != nil {
		s.nu.Reply(p.tid, res)
		return
	}
	r.RAX = uint64(res)
	if res == -int64(syscall.EINTR) && act.flags&saRestart != 0 {
		r.RIP -= 2 // back onto the SYSCALL instruction: the call runs again
		r.RAX = nr
	}
	if p.deliver(sig, act, info, &r) {
		s.nu.Reply(p.tid, int64(r.RAX))
	}
}

// deliver runs sig's handler from registers r (written back to the
// thread), or its default action. false: the process is gone.
func (p *Process) deliver(sig int, act sigaction, info sigInfo, r *Regs) bool {
	switch {
	case act.handler == sigIGN || act.handler == sigDFL && ignoredByDefault(sig):
		return p.resume(r)
	case act.handler == sigDFL || sig == sigKILL:
		p.exit(0, sig, "")
		return false
	}
	p.mu.Lock()
	old := p.sigMask
	if p.suspended { // rt_sigsuspend: the handler returns to the mask before the call
		old, p.suspended = p.suspendMask, false
	}
	if act.flags&saNodefer == 0 {
		p.sigMask |= bit(sig)
	}
	p.sigMask |= act.mask &^ unblockable
	if act.flags&saResethand != 0 {
		p.actions[sig] = sigaction{}
	}
	p.mu.Unlock()
	h, err := p.pushFrame(*r, sig, act, old, info)
	if err != nil { // no room on the stack: as Linux, the process dies
		p.exit(0, sigSEGV, "no stack for a signal frame")
		return false
	}
	*r = h
	return p.resume(r)
}

func (p *Process) resume(r *Regs) bool {
	if err := p.sys.nu.Regs(p.tid, r, true); err != nil {
		p.sys.log.Errorf("pid %d: registers: %v", p.PID, err)
	}
	return true
}

// stopped answers an interrupt or fault stop: deliver what is pending.
func (p *Process) stopped(fault bool, sig int, info sigInfo, rip uint64) {
	s := p.sys
	if fault {
		p.mu.Lock()
		act, blocked := p.actions[sig], p.sigMask&bit(sig) != 0
		p.mu.Unlock()
		if act.handler == sigDFL || act.handler == sigIGN || blocked { // a fault cannot be ignored
			p.exit(0, sig, fmt.Sprintf("%s at %#x (rip %#x)", info.what, info.addr, rip))
			return
		}
		var r Regs
		if s.nu.Regs(p.tid, &r, false) == nil && p.deliver(sig, act, info, &r) {
			s.nu.Reply(p.tid, 0)
		}
		return
	}
	sig, act, si := p.takeSignal()
	if sig != 0 {
		var r Regs
		if s.nu.Regs(p.tid, &r, false) != nil || !p.deliver(sig, act, si, &r) {
			return
		}
	}
	s.nu.Reply(p.tid, 0)
}

// ---- system calls ----

func sysRtSigactionFn(p *Process, a [6]uint64) (int64, bool) {
	sig := int(a[0])
	if sig < 1 || sig > nSig || a[3] != 8 || a[1] != 0 && (sig == sigKILL || sig == sigSTOP) {
		return ok(-int64(syscall.EINVAL))
	}
	var b [32]byte
	if a[1] != 0 {
		if err := p.mem.CopyIn(a[1], b[:]); err != nil {
			return ok(errno(err))
		}
	}
	p.mu.Lock()
	old := p.actions[sig]
	if a[1] != 0 {
		le := binary.LittleEndian
		p.actions[sig] = sigaction{le.Uint64(b[0:]), le.Uint64(b[8:]), le.Uint64(b[16:]), le.Uint64(b[24:])}
		if p.actions[sig].handler == sigIGN || p.actions[sig].handler == sigDFL && ignoredByDefault(sig) {
			p.pending &^= bit(sig) // POSIX: setting "ignore" discards it
		}
	}
	p.mu.Unlock()
	if a[2] != 0 {
		le := binary.LittleEndian
		le.PutUint64(b[0:], old.handler)
		le.PutUint64(b[8:], old.flags)
		le.PutUint64(b[16:], old.restorer)
		le.PutUint64(b[24:], old.mask)
		return ok(errno(p.mem.CopyOut(a[2], b[:])))
	}
	return ok(0)
}

func sysRtSigprocmaskFn(p *Process, a [6]uint64) (int64, bool) {
	var b [8]byte
	var set uint64
	if a[1] != 0 {
		if err := p.mem.CopyIn(a[1], b[:]); err != nil {
			return ok(errno(err))
		}
		set = binary.LittleEndian.Uint64(b[:]) &^ unblockable
	}
	p.mu.Lock()
	old := p.sigMask
	if a[1] != 0 {
		switch a[0] {
		case 0: // SIG_BLOCK
			p.sigMask |= set
		case 1: // SIG_UNBLOCK
			p.sigMask &^= set
		case 2: // SIG_SETMASK
			p.sigMask = set
		default:
			p.mu.Unlock()
			return ok(-int64(syscall.EINVAL))
		}
		if p.pending&^p.sigMask != 0 {
			p.wakeLocked() // an unblocked pending signal is delivered on return
		}
	}
	p.mu.Unlock()
	if a[2] != 0 {
		binary.LittleEndian.PutUint64(b[:], old)
		return ok(errno(p.mem.CopyOut(a[2], b[:])))
	}
	return ok(0)
}

// rt_sigreturn: back to the context the handler interrupted.
func sysRtSigreturnFn(p *Process, _ [6]uint64) (int64, bool) {
	var r Regs
	if err := p.sys.nu.Regs(p.tid, &r, false); err != nil {
		return ok(errno(err))
	}
	restored, mask, err := p.popFrame(r)
	if err != nil || restored.RIP >= UserTop || restored.RSP >= UserTop {
		p.exit(0, sigSEGV, "bad signal frame")
		return 0, true
	}
	p.mu.Lock()
	p.sigMask = mask &^ unblockable
	p.mu.Unlock()
	if err := p.sys.nu.Regs(p.tid, &restored, true); err != nil {
		return ok(errno(err))
	}
	return ok(int64(restored.RAX)) // the reply puts the interrupted RAX back
}

// kill(pid, sig): pid > 0 one process, 0 our group, -1 everyone else,
// < -1 group -pid. sig 0 only checks that the target exists.
func sysKillFn(p *Process, a [6]uint64) (int64, bool) {
	return ok(p.sys.kill(p, int64(int32(a[0])), int(a[1])))
}

func (s *System) kill(from *Process, pid int64, sig int) int64 {
	if sig < 0 || sig > nSig {
		return -int64(syscall.EINVAL)
	}
	s.mu.Lock()
	var targets []*Process
	for _, t := range s.byPID {
		if t.zombie {
			continue
		}
		switch {
		case pid > 0 && int64(t.PID) == pid,
			pid == 0 && from != nil && t.PGID == from.PGID,
			pid == -1 && t != from && t.PID != 1,
			pid < -1 && int64(t.PGID) == -pid:
			targets = append(targets, t)
		}
	}
	s.mu.Unlock()
	if len(targets) == 0 {
		return -int64(syscall.ESRCH)
	}
	sender := int32(0)
	if from != nil {
		sender = int32(from.PID)
	}
	for _, t := range targets {
		if sig != 0 {
			t.send(sig, sigInfo{signo: int32(sig), code: siUser, pid: sender})
		}
	}
	return 0
}

// Kill is the kernel's own kill (the TTY's ^C, the console).
func (s *System) Kill(pid int64, sig int) error {
	if r := s.kill(nil, pid, sig); r < 0 {
		return syscall.Errno(-r)
	}
	return nil
}

func sysTgkillFn(p *Process, a [6]uint64) (int64, bool) {
	return ok(p.sys.kill(p, int64(int32(a[1])), int(a[2]))) // tid = pid: one thread each
}

func sysTkillFn(p *Process, a [6]uint64) (int64, bool) {
	return ok(p.sys.kill(p, int64(int32(a[0])), int(a[1])))
}

// rt_sigsuspend: wait with a temporary mask until a signal is delivered.
func sysRtSigsuspendFn(p *Process, a [6]uint64) (int64, bool) {
	var b [8]byte
	if err := p.mem.CopyIn(a[0], b[:]); err != nil {
		return ok(errno(err))
	}
	p.mu.Lock()
	old := p.sigMask
	p.sigMask = binary.LittleEndian.Uint64(b[:]) &^ unblockable
	if p.pending&^p.sigMask != 0 {
		p.wakeLocked()
	}
	p.suspendMask, p.suspended = old, true // finish puts it in the frame
	wake := p.sigWake
	p.mu.Unlock()
	<-wake
	return ok(-int64(syscall.EINTR))
}

// pause: like sigsuspend with the current mask.
func sysPauseFn(p *Process, _ [6]uint64) (int64, bool) {
	<-p.cancel()
	return ok(-int64(syscall.EINTR))
}

// nanosleep, interruptible.
func (p *Process) sleep(d time.Duration) int64 {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
		return 0
	case <-p.cancel():
		return -int64(syscall.EINTR)
	}
}

func sysSigaltstackFn(*Process, [6]uint64) (int64, bool) { return ok(0) } // no alternate stacks yet

// Linux numbers of the signal calls.
func init() {
	for nr, h := range map[uint64]handler{
		13: sysRtSigactionFn, 14: sysRtSigprocmaskFn, 15: sysRtSigreturnFn, 34: sysPauseFn,
		62: sysKillFn, 130: sysRtSigsuspendFn, 131: sysSigaltstackFn, 200: sysTkillFn, 234: sysTgkillFn,
	} {
		table[nr] = h
	}
}
