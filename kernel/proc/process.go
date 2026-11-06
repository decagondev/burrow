package proc

import (
	"path"
	"sync"
	"time"

	"burrow/kernel/tty"
)

// Signal numbers used before W10-S04 brings the rest.
const (
	sigILL  = 4
	sigFPE  = 8
	sigSEGV = 11
	sigCHLD = 17
)

// Process is one program: its threads (thread.go), memory, descriptors,
// and its place in the process tree.
type Process struct {
	PID, PPID, PGID, SID int
	Path                 string
	sys                  *System
	mem                  *Mem
	fds                  *fdTable

	mu            sync.Mutex
	threads       map[int]*Thread // by TID
	main          *Thread         // the thread whose TID is the pid (tests, TID())
	brkStart, brk uint64
	mmapNext      uint64
	cwd           string
	umask         uint32        // umask(2); applied from W19-S04
	cred          Cred          // who it is (W19-S03, cred.go); the zero value is root
	timers        [3]itimer     // setitimer's ITIMER_REAL, _VIRTUAL, _PROF (itimer.go)
	ctty          *tty.TTY      // the controlling terminal (TIOCSCTTY, ptycalls.go); nil: the console
	argv, envp    []string      // as execve gave them, for /proc/<pid>/cmdline and environ (W23-S01)
	start         time.Duration // since boot (/proc/<pid>/stat's starttime)
	ended         cpuTimes      // CPU time of its threads that have ended (cputime.go, W23-S02)
	final         cpuTimes      // its own CPU time, fixed at exit before it is a zombie
	finalRSS      uint64        // its memory at exit (wait4's ru_maxrss)
	kids          cpuTimes      // its reaped children's CPU time, and theirs
	kidsRSS       uint64

	children   map[int]*Process // live and zombie (s.mu)
	childEvent chan struct{}    // a child exited (capacity 1)
	zombie     bool             // exited, waiting to be reaped (s.mu)
	exitCode   int
	termSig    int
	reason     string
	done       chan struct{}

	// signals (p.mu): actions are shared by the threads; pending here are
	// process-directed (kill), thread-directed ones are on the Thread
	actions [nSig + 1]sigaction
	info    [nSig + 1]sigInfo
	pending uint64
	exited  bool
}

// Wait blocks until the process has exited and returns a shell's view of
// it: the exit code, or 128 + the signal that killed it (with the reason).
func (p *Process) Wait() (status int, reason string) {
	<-p.done
	if p.termSig != 0 {
		return 128 + p.termSig, p.reason
	}
	return p.exitCode, ""
}

// TermSig is the signal that ended the process (0 if it exited), once
// Wait has returned.
func (p *Process) TermSig() int { <-p.done; return p.termSig }

// waitStatus is Linux's wait4 status word.
func (p *Process) waitStatus() uint64 {
	if p.termSig != 0 {
		return uint64(p.termSig & 0x7F)
	}
	return uint64(p.exitCode&0xFF) << 8
}

// Mem is the process's memory (tests use it to stage syscall arguments).
func (p *Process) Mem() *Mem { return p.mem }

// TID is the main thread's nucleus thread (0 once it has gone).
func (p *Process) TID() uint64 {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.main == nil {
		return 0
	}
	return p.main.nt
}

// exit ends the process: every thread, memory, descriptors. It stays a
// zombie until its parent reaps it (init reaps at once); its children go
// to init.
func (p *Process) exit(code, sig int, reason string) {
	s := p.sys
	p.mu.Lock()
	if p.exited { // two threads ending the process at once: the first wins
		p.mu.Unlock()
		return
	}
	p.exited = true
	threads := make([]*Thread, 0, len(p.threads))
	for _, t := range p.threads {
		t.exited = true
		t.wakeLocked() // a handler blocked for this thread returns, and answers nobody (BUG-0026)
		threads = append(threads, t)
	}
	p.threads = map[int]*Thread{}
	p.mu.Unlock()
	p.endThreads(threads) // their CPU time, while the nucleus still has them (W23-S02)
	p.mu.Lock()
	p.final = p.ended
	p.mu.Unlock()
	s.mu.Lock()
	for _, t := range threads {
		delete(s.byTID, t.nt)
		s.gone[t.nt] = true
	}
	s.mu.Unlock()
	for _, t := range threads {
		if err := s.nu.Kill(t.nt); err != nil {
			s.log.Errorf("pid %d tid %d: kill: %v", p.PID, t.TID, err)
		}
	}
	s.futexDrop(p.mem)
	s.releaseVT(p.PID) // a crashed graphics program gives the console back (W14-S08)
	p.stopTimers()
	_, p.finalRSS = p.mem.usage()
	if err := p.mem.Close(); err != nil {
		s.log.Errorf("pid %d: free memory: %v", p.PID, err)
	}
	p.fds.closeAll()
	if reason != "" {
		s.log.Warnf("pid %d (%s) killed: %s", p.PID, path.Base(p.Path), reason)
	}
	s.mu.Lock()
	p.exitCode, p.termSig, p.reason = code, sig, reason
	adopted := false
	for pid, c := range p.children {
		c.PPID = 1
		s.init.children[pid] = c
		adopted = adopted || c.zombie
	}
	p.children = nil
	if adopted { // zombies it never reaped: init does it now
		select {
		case s.init.childEvent <- struct{}{}:
		default:
		}
	}
	parent := s.byPID[p.PPID]
	if parent != nil && !parent.zombie {
		p.zombie = true
		select {
		case parent.childEvent <- struct{}{}:
		default: // already signalled
		}
	} else {
		delete(s.byPID, p.PID) // nobody will wait: reaped now
	}
	s.mu.Unlock()
	close(p.done)
	if parent != nil {
		parent.send(sigCHLD, sigInfo{signo: sigCHLD, code: 1 /* CLD_EXITED */, pid: int32(p.PID)})
	}
}

// fork: a copy of p (memory, descriptors, and the calling thread's
// registers with RAX = 0) as its child, with one thread.
func (t *Thread) fork() int64 { return t.forkOnto(0) }

// forkOnto is fork whose child starts on stack (0: the caller's). clone's
// process shape passes one: musl's posix_spawn (and so system()) clones with
// CLONE_VM|CLONE_VFORK onto a stack holding the function the child runs,
// and a child on the parent's stack jumped to garbage (BUG-0028).
func (t *Thread) forkOnto(stack uint64) int64 {
	p, s := t.p, t.p.sys
	var regs Regs
	if err := s.nu.Regs(t.nt, &regs, false); err != nil {
		return errno(err)
	}
	mem, err := p.mem.Clone()
	if err != nil {
		return errno(err)
	}
	c := s.newProcess(p.Path, mem, p.fds.clone())
	p.mu.Lock()
	c.brkStart, c.brk, c.mmapNext, c.cwd, c.umask = p.brkStart, p.brk, p.mmapNext, p.cwd, p.umask
	c.ctty = p.ctty
	c.argv, c.envp = p.argv, p.envp // never changed in place: execve replaces them
	c.cred = p.cred.clone()
	c.actions = p.actions // pending signals are not inherited
	mask, fs, altSP, altSize := t.sigMask, t.fs, t.altSP, t.altSize
	p.mu.Unlock()
	c.PPID, c.PGID, c.SID = p.PID, p.PGID, p.SID
	regs.RAX = 0
	if stack != 0 {
		regs.RSP = stack
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	tid, err := s.nu.StartThreadFrom(mem.Space(), &regs)
	if err != nil {
		mem.Close()
		c.fds.closeAll()
		return errno(err)
	}
	ct := s.register(c, tid)
	ct.sigMask, ct.fs, ct.altSP, ct.altSize = mask, fs, altSP, altSize
	p.children[c.PID] = c
	return int64(c.PID)
}

const (
	wNoHang     = 1
	maxExecArgs = 256
	maxExecSize = 128 << 10
)

// wait4(pid, status, options, rusage): pid > 0 that child, -1 any child,
// 0 any in our group, < -1 any in group -pid. Blocks unless WNOHANG.
func (t *Thread) wait4(pid int64, statusVA, options, rusageVA uint64) int64 {
	p := t.p
	found, e := t.waitChild(func(c *Process) bool {
		return !(pid > 0 && int64(c.PID) != pid || pid == 0 && c.PGID != p.PGID || pid < -1 && int64(c.PGID) != -pid)
	}, options&wNoHang != 0, false)
	if found == nil {
		return e // -ECHILD, -EINTR, or 0 for WNOHANG
	}
	if err := p.childUsage(found, rusageVA); err != nil {
		return errno(err)
	}
	if statusVA != 0 {
		var b [4]byte
		st := found.waitStatus()
		b[0], b[1] = byte(st), byte(st>>8)
		if err := p.mem.CopyOut(statusVA, b[:]); err != nil {
			return errno(err)
		}
	}
	return int64(found.PID)
}
