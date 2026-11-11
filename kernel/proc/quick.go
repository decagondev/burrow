package proc

// quick.go - calls answered on the dispatcher itself (W25-S03): a request
// whose handler can never block (it only reads a process's or thread's
// state under a short lock) is served where the dispatcher took it, with
// no goroutine started for it and no Gosched before the next one runs,
// which is most of what such a call cost (W25-S01's trace). Its answer goes
// to the nucleus with the dispatcher's next wait, in one crossing
// (nu_syscall_reply_wait). Everything a call's ending does is kept: the
// thread's state for /proc and the dump, the signals that run before the
// thread returns (finish). A call that may wait for anything (I/O, a child,
// a timer, a lock another call holds while it waits) must never be listed
// here: it would stall every other request behind it.
// Docs: docs/04-plan/waves/WAVE-25-speed.md.

// quickCalls are served on the dispatcher (Linux numbers).
var quickCalls = map[uint64]bool{
	24:  true, // sched_yield
	39:  true, // getpid
	102: true, // getuid
	104: true, // getgid
	107: true, // geteuid
	108: true, // getegid
	110: true, // getppid
	186: true, // gettid
	96:  true, // gettimeofday (the vDSO's fallback)
	201: true, // time
	228: true, // clock_gettime
	229: true, // clock_getres
}

// reply is an answer the dispatcher still owes (tid 0: none).
type reply struct {
	tid uint64
	res int64
}

// quick serves r on the dispatcher and says whether it did; a request it
// declines goes to its own goroutine as before. An answer it leaves is put
// in *owed, after any answer already there is sent.
func (s *System) quick(r Request, owed *reply) bool {
	if r.IsFault() || r.Nr == InterruptNr || !quickCalls[r.Nr] {
		return false
	}
	s.mu.Lock()
	t := s.byTID[r.TID]
	s.mu.Unlock()
	if t == nil || t.signalReady() {
		// unknown or gone: handle decides what to do; a signal to deliver may
		// end the process (closing its files), too much for the dispatcher
		return false
	}
	t.enter(r.Nr)
	res, exited := t.syscall(r.Nr, r.Args)
	t.leave()
	if !exited && t.finishLeavingReply(res, r.Nr) {
		if owed.tid != 0 {
			s.nu.Reply(owed.tid, owed.res)
		}
		*owed = reply{t.nt, res}
	}
	return true
}

// signalReady says whether t has a signal it would take now (takeSignal's
// test, without taking it). One sent between this test and finish is still
// delivered on the dispatcher: slower, never wrong.
func (t *Thread) signalReady() bool {
	p := t.p
	p.mu.Lock()
	defer p.mu.Unlock()
	return (t.pending|p.pending)&^(t.sigMask&^unblockable) != 0
}

// wait takes the next requests, sending an owed answer in the same crossing.
func (s *System) wait(buf []Request, owed *reply) (int, error) {
	if owed.tid == 0 {
		return s.nu.Wait(buf)
	}
	tid, res := owed.tid, owed.res
	*owed = reply{}
	return s.nu.ReplyWait(tid, res, buf)
}
