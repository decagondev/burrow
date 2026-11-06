package proc

// waitid.go - waitid(2) (W15-S02: Qt's QProcess reaps its children with
// it) and the child search it shares with wait4. Only exits are reported:
// there are no stopped processes yet, so WSTOPPED and WCONTINUED find
// nothing. Docs: docs/02-architecture/POSIX_COVERAGE.md.

import (
	"encoding/binary"
	"syscall"
)

const (
	pAll  = 0
	pPID  = 1
	pPGID = 2

	wExited    = 4
	wStopped   = 2
	wContinued = 8
	wNoWait    = 0x01000000

	cldExited   = 1
	cldKilled   = 2
	siginfoSize = 128
)

// waitChild waits for a child that match accepts to be a zombie and
// returns it, reaped unless keep. -ECHILD when no child matches; with
// noHang, (nil, 0) when none has exited yet; -EINTR on a signal.
func (t *Thread) waitChild(match func(c *Process) bool, noHang, keep bool) (*Process, int64) {
	p, s := t.p, t.p.sys
	for {
		s.mu.Lock()
		var found *Process
		any := false
		for _, c := range p.children {
			if !match(c) {
				continue
			}
			any = true
			if c.zombie {
				found = c
				break
			}
		}
		if found != nil && !keep {
			delete(p.children, found.PID)
			delete(s.byPID, found.PID)
		}
		s.mu.Unlock()
		switch {
		case found != nil:
			if !keep {
				p.reaped(found) // its CPU time joins ours (W23-S02)
			}
			return found, 0
		case !any:
			return nil, -int64(syscall.ECHILD)
		case noHang:
			return nil, 0
		}
		select {
		case <-p.childEvent:
		case <-t.cancel():
			return nil, -int64(syscall.EINTR)
		}
	}
}

// waitid(idtype, id, siginfo_t *info, options, rusage).
func sysWaitidFn(t *Thread, a [6]uint64) (int64, bool) {
	p := t.p
	idtype, id, infoVA, options := a[0], int(int32(a[1])), a[2], a[3]
	var match func(c *Process) bool
	switch idtype {
	case pAll:
		match = func(*Process) bool { return true }
	case pPID:
		match = func(c *Process) bool { return c.PID == id }
	case pPGID:
		if id == 0 {
			id = p.PGID
		}
		match = func(c *Process) bool { return c.PGID == id }
	default:
		return ok(-int64(syscall.EINVAL)) // P_PIDFD: no pidfds
	}
	if options&(wExited|wStopped|wContinued) == 0 || options&^(wExited|wStopped|wContinued|wNoHang|wNoWait) != 0 {
		return ok(-int64(syscall.EINVAL))
	}
	if options&wExited == 0 { // only stops or continues asked for: none ever happen
		return ok(t.waitNever(match, infoVA, options&wNoHang != 0))
	}
	c, e := t.waitChild(match, options&wNoHang != 0, options&wNoWait != 0)
	if e < 0 {
		return ok(e)
	}
	if c != nil {
		if err := p.childUsage(c, a[4]); err != nil {
			return ok(errno(err))
		}
	}
	if infoVA != 0 {
		return ok(errno(p.mem.CopyOut(infoVA, siginfo(c))))
	}
	return ok(0)
}

// waitNever answers a waitid for stops or continues alone: with a child
// to wait for, nothing to report (WNOHANG) or a wait only a signal ends.
func (t *Thread) waitNever(match func(c *Process) bool, infoVA uint64, noHang bool) int64 {
	p := t.p
	p.sys.mu.Lock()
	any := false
	for _, c := range p.children {
		any = any || match(c)
	}
	p.sys.mu.Unlock()
	switch {
	case !any:
		return -int64(syscall.ECHILD)
	case noHang:
		return errno(p.mem.CopyOut(infoVA, siginfo(nil)))
	}
	<-t.cancel()
	return -int64(syscall.EINTR)
}

// siginfo is the siginfo_t waitid fills for an exited child (all zeros
// when none has exited yet: WNOHANG).
func siginfo(c *Process) []byte {
	b := make([]byte, siginfoSize)
	if c == nil {
		return b
	}
	le := binary.LittleEndian
	le.PutUint32(b[0:], sigCHLD)
	code, status := uint32(cldExited), uint32(c.exitCode)
	if c.termSig != 0 {
		code, status = cldKilled, uint32(c.termSig)
	}
	le.PutUint32(b[8:], code)
	le.PutUint32(b[16:], uint32(c.PID))        // si_pid
	le.PutUint32(b[20:], c.Cred().UID[idReal]) // si_uid (W19-S03)
	le.PutUint32(b[24:], status)
	return b
}

func init() { ttable[247] = sysWaitidFn }
