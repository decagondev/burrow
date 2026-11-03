package proc

// sigcalls.go - the signal system calls (W10-S04; per thread since
// W12-S01): sigaction (shared by the threads), sigprocmask, sigreturn,
// sigsuspend, pause and sigaltstack (the calling thread's), kill, tkill and
// tgkill. Delivery is in signal.go. Docs: POSIX_COVERAGE.md (Signals).

import (
	"encoding/binary"
	"syscall"
	"time"
)

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
		if p.discarded(sig) { // POSIX: setting "ignore" discards what is pending
			p.pending &^= bit(sig)
			for _, t := range p.threads {
				t.pending &^= bit(sig)
			}
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

func sysRtSigprocmaskFn(t *Thread, a [6]uint64) (int64, bool) {
	p := t.p
	var b [8]byte
	var set uint64
	if a[1] != 0 {
		if err := p.mem.CopyIn(a[1], b[:]); err != nil {
			return ok(errno(err))
		}
		set = binary.LittleEndian.Uint64(b[:]) &^ unblockable
	}
	p.mu.Lock()
	old := t.sigMask
	if a[1] != 0 {
		switch a[0] {
		case 0: // SIG_BLOCK
			t.sigMask |= set
		case 1: // SIG_UNBLOCK
			t.sigMask &^= set
		case 2: // SIG_SETMASK
			t.sigMask = set
		default:
			p.mu.Unlock()
			return ok(-int64(syscall.EINVAL))
		}
		if t.deliverableLocked() {
			t.wakeLocked() // an unblocked pending signal is delivered on return
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
func sysRtSigreturnFn(t *Thread, _ [6]uint64) (int64, bool) {
	p := t.p
	var r Regs
	if err := p.sys.nu.Regs(t.nt, &r, false); err != nil {
		return ok(errno(err))
	}
	restored, mask, err := p.popFrame(r)
	if err != nil || restored.RIP >= UserTop || restored.RSP >= UserTop {
		p.exit(0, sigSEGV, "bad signal frame")
		return 0, true
	}
	p.mu.Lock()
	t.sigMask = mask &^ unblockable
	if t.deliverableLocked() {
		t.wakeLocked()
	}
	p.mu.Unlock()
	if err := p.sys.nu.Regs(t.nt, &restored, true); err != nil {
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

// KillGroup signals every process in group pgrp (the TTY's ^C). Not
// Kill(-pgrp): for group 1 that would be kill(-1), "everyone".
func (s *System) KillGroup(pgrp, sig int) {
	s.mu.Lock()
	var targets []*Process
	for _, t := range s.byPID {
		if !t.zombie && t.PGID == pgrp {
			targets = append(targets, t)
		}
	}
	s.mu.Unlock()
	for _, t := range targets {
		t.send(sig, sigInfo{signo: int32(sig), code: siKernel})
	}
}

// Kill is the kernel's own kill (the console).
func (s *System) Kill(pid int64, sig int) error {
	if r := s.kill(nil, pid, sig); r < 0 {
		return syscall.Errno(-r)
	}
	return nil
}

// thread finds thread tid, in process tgid unless tgid is -1.
func (s *System) thread(tgid int64, tid int) *Thread {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, p := range s.byPID {
		if tgid != -1 && int64(p.PID) != tgid {
			continue
		}
		p.mu.Lock()
		t := p.threads[tid]
		p.mu.Unlock()
		if t != nil && !t.exited {
			return t
		}
	}
	return nil
}

// tgkill(tgid, tid, sig) and tkill(tid, sig): a signal for one thread.
func sysTgkillFn(t *Thread, a [6]uint64) (int64, bool) {
	return ok(t.p.sys.threadKill(t, int64(int32(a[0])), int(int32(a[1])), int(a[2])))
}

func sysTkillFn(t *Thread, a [6]uint64) (int64, bool) {
	return ok(t.p.sys.threadKill(t, -1, int(int32(a[0])), int(a[1])))
}

func (s *System) threadKill(from *Thread, tgid int64, tid, sig int) int64 {
	if sig < 0 || sig > nSig || tid <= 0 || tgid == 0 || tgid < -1 {
		return -int64(syscall.EINVAL)
	}
	target := s.thread(tgid, tid)
	if target == nil {
		return -int64(syscall.ESRCH)
	}
	if sig != 0 {
		target.send(sig, sigInfo{signo: int32(sig), code: siTkill, pid: int32(from.p.PID)})
	}
	return 0
}

// rt_sigsuspend: wait with a temporary mask until a signal is delivered.
func sysRtSigsuspendFn(t *Thread, a [6]uint64) (int64, bool) {
	p := t.p
	var b [8]byte
	if err := p.mem.CopyIn(a[0], b[:]); err != nil {
		return ok(errno(err))
	}
	p.mu.Lock()
	old := t.sigMask
	t.sigMask = binary.LittleEndian.Uint64(b[:]) &^ unblockable
	if t.deliverableLocked() {
		t.wakeLocked()
	}
	t.suspendMask, t.suspended = old, true // finish puts it in the frame
	wake := t.sigWake
	p.mu.Unlock()
	<-wake
	return ok(-int64(syscall.EINTR))
}

// pause: like sigsuspend with the current mask.
func sysPauseFn(t *Thread, _ [6]uint64) (int64, bool) {
	<-t.cancel()
	return ok(-int64(syscall.EINTR))
}

// nanosleep, interruptible.
func (t *Thread) sleep(d time.Duration) int64 {
	tm := time.NewTimer(d)
	defer tm.Stop()
	select {
	case <-tm.C:
		return 0
	case <-t.cancel():
		return -int64(syscall.EINTR)
	}
}

// Linux numbers of the signal calls.
func init() {
	table[13] = sysRtSigactionFn
	table[62] = sysKillFn
	for nr, h := range map[uint64]thandler{
		14: sysRtSigprocmaskFn, 15: sysRtSigreturnFn, 34: sysPauseFn, 130: sysRtSigsuspendFn,
		131: sysSigaltstackFn, 200: sysTkillFn, 234: sysTgkillFn,
	} {
		ttable[nr] = h
	}
}
