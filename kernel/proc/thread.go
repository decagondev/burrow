package proc

// thread.go - one thread of a process (W12-S01): what Linux keeps per
// thread. A process has one Thread per nucleus thread; the main thread's
// TID is the pid, others take TIDs from the same number space. A thread's
// signal state is guarded by its process's mu. Handlers that need the
// calling thread (signals, TLS, exit, clone, blocking calls that a signal
// interrupts) are thandlers in ttable; the rest take the Process.
// Docs: docs/04-plan/waves/WAVE-12-ports.md.

import (
	"encoding/binary"
	"path"
	"sync/atomic"
	"syscall"
)

// Thread is one thread of a process.
type Thread struct {
	p   *Process
	nt  uint64 // the nucleus thread
	TID int    // the Linux thread id

	// guarded by p.mu
	sigMask     uint64 // blocked signals
	pending     uint64 // thread-directed signals (tgkill)
	info        [nSig + 1]sigInfo
	sigWake     chan struct{} // closed while a signal for this thread is deliverable
	suspendMask uint64        // rt_sigsuspend: the mask to return to
	suspended   bool
	fs          uint64 // TLS base
	altSP       uint64 // sigaltstack: base and size (size 0: none)
	altSize     uint64
	clearTID    uint64 // set_tid_address / CLONE_CHILD_CLEARTID
	robustList  uint64 // set_robust_list's head (robust.go, W24-S02)
	exited      bool

	inNr    atomic.Int64 // the system call in flight, -1 for none (dump.go)
	inSince atomic.Int64 // when it began, Unix nanoseconds
}

// thandler is a system call that needs the calling thread.
type thandler func(t *Thread, a [6]uint64) (res int64, exited bool)

var ttable = map[uint64]thandler{}

// addThread makes nucleus thread nt thread tid of p and publishes it
// (s.mu held).
func (p *Process) addThread(nt uint64, tid int) *Thread {
	t := &Thread{p: p, nt: nt, TID: tid, sigWake: make(chan struct{})}
	t.inNr.Store(-1)
	p.mu.Lock()
	p.threads[tid] = t
	if tid == p.PID {
		p.main = t
	}
	p.mu.Unlock()
	p.sys.byTID[nt] = t
	delete(p.sys.gone, nt) // a reused nucleus thread number is live again
	return t
}

// cancel is closed when a signal interrupts t's blocking calls (EINTR).
func (t *Thread) cancel() <-chan struct{} {
	t.p.mu.Lock()
	defer t.p.mu.Unlock()
	return t.sigWake
}

// wakeLocked closes sigWake once (p.mu held); takeSignal makes a new one.
func (t *Thread) wakeLocked() {
	select {
	case <-t.sigWake:
	default:
		close(t.sigWake)
	}
}

// syscall runs one call for t.
func (t *Thread) syscall(nr uint64, a [6]uint64) (int64, bool) {
	if h, found := ttable[nr]; found {
		return h(t, a)
	}
	h, found := table[nr]
	if !found {
		t.p.sys.unknown(t.p, nr)
		return ok(-int64(syscall.ENOSYS))
	}
	return h(t.p, a)
}

// live lists p's threads that have not exited.
func (p *Process) live() []*Thread {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([]*Thread, 0, len(p.threads))
	for _, t := range p.threads {
		if !t.exited {
			out = append(out, t)
		}
	}
	return out
}

func sysGettidFn(t *Thread, _ [6]uint64) (int64, bool) { return ok(int64(t.TID)) }

// set_tid_address: where to write 0 and wake a futex when t exits.
func sysSetTidAddressFn(t *Thread, a [6]uint64) (int64, bool) {
	t.p.mu.Lock()
	t.clearTID = a[0]
	t.p.mu.Unlock()
	return ok(int64(t.TID))
}

// exitThread ends t alone (exit(2)); the last thread to go ends the process.
func (t *Thread) exitThread(code int) {
	p, s := t.p, t.p.sys
	p.mu.Lock()
	t.exited = true
	last := true
	for _, o := range p.threads {
		last = last && o.exited
	}
	clear := t.clearTID
	p.mu.Unlock()
	if last {
		p.exit(code, 0, "")
		return
	}
	if clear != 0 { // CLONE_CHILD_CLEARTID: pthread_join waits on this word
		if p.mem.CopyOut(clear, []byte{0, 0, 0, 0}) == nil {
			s.futexWake(p.mem, clear, 1, ^uint32(0))
		}
	}
	s.mu.Lock()
	delete(s.byTID, t.nt)
	s.gone[t.nt] = true
	s.mu.Unlock()
	p.endThreads([]*Thread{t}) // its CPU time stays the process's (W23-S02)
	p.mu.Lock()
	delete(p.threads, t.TID)
	p.mu.Unlock()
	if err := s.nu.Kill(t.nt); err != nil {
		s.log.Errorf("pid %d tid %d: kill: %v", p.PID, t.TID, err)
	}
}

func sysExitThreadFn(t *Thread, a [6]uint64) (int64, bool) {
	t.exitThread(int(a[0] & 0xFF))
	return 0, true
}

func sysExitGroupFn(t *Thread, a [6]uint64) (int64, bool) {
	t.p.exit(int(a[0]&0xFF), 0, "")
	return 0, true
}

// sched_yield: the nucleus schedules ring-3 threads; answering is a yield.
func sysSchedYieldFn(*Thread, [6]uint64) (int64, bool) { return ok(0) }

// sched_getaffinity: every CPU online (W20-S08: the nucleus tells the Go
// runtime, whose NumCPU is the count; nproc and sysconf follow).
func sysSchedGetaffinityFn(t *Thread, a [6]uint64) (int64, bool) {
	if a[1] < 8 {
		return ok(-int64(syscall.EINVAL))
	}
	var mask [8]byte
	binary.LittleEndian.PutUint64(mask[:], onlineMask())
	if err := t.p.mem.CopyOut(a[2], mask[:]); err != nil {
		return ok(errno(err))
	}
	return ok(8) // bytes of mask written

}

func init() {
	for nr, h := range map[uint64]thandler{
		60: sysExitThreadFn, 231: sysExitGroupFn, 186: sysGettidFn, 218: sysSetTidAddressFn,
		24: sysSchedYieldFn, 204: sysSchedGetaffinityFn,
	} {
		ttable[nr] = h
	}
}

const (
	archSetFS = 0x1002
	archGetFS = 0x1003
	prSetName = 15 // prctl options
	prGetName = 16
)

func sysArchPrctlFn(t *Thread, a [6]uint64) (int64, bool) {
	p := t.p
	switch a[0] {
	case archSetFS:
		if err := p.sys.nu.SetFS(t.nt, a[1]); err != nil {
			return ok(errno(err))
		}
		p.mu.Lock()
		t.fs = a[1]
		p.mu.Unlock()
		return ok(0)
	case archGetFS:
		p.mu.Lock()
		fs := t.fs
		p.mu.Unlock()
		var b [8]byte
		binary.LittleEndian.PutUint64(b[:], fs)
		return ok(errno(p.mem.CopyOut(a[1], b[:])))
	}
	return ok(-int64(syscall.EINVAL))
}

// prctl: only the thread name. PR_SET_NAME is accepted and ignored (ps
// shows the program path); PR_GET_NAME returns the path's base name.
func sysPrctlFn(p *Process, a [6]uint64) (int64, bool) {
	switch a[0] {
	case prSetName:
		return ok(0)
	case prGetName:
		p.mu.Lock()
		name := make([]byte, 16) // TASK_COMM_LEN, NUL-padded
		copy(name[:15], path.Base(p.Path))
		p.mu.Unlock()
		return ok(errno(p.mem.CopyOut(a[1], name)))
	}
	return ok(-int64(syscall.EINVAL))
}
