package proc

// procsource.go - the System as /proc's Source (W23-S01): the pids, a
// snapshot of one process (taken under s.mu, then p.mu, then the memory's
// own lock, never two at once beyond that order), the uptime, the load
// average sampled every five seconds as Linux does, memory and uname. A
// thread counts as running unless it has been in a call that waits (read,
// wait4, nanosleep, poll, futex, ...) for more than blockedAfter; one
// opening its own stat is in openat, so running, as on Linux, however
// loaded the machine. Never changes a process. Docs: docs/04-plan/waves/WAVE-23-proc-virtio.md.

import (
	"math"
	"path"
	"sort"
	"time"

	"burrow/kernel/procfs"
	"burrow/kernel/tty"
)

const (
	loadSample   = 5 * time.Second // Linux's LOAD_FREQ
	ttyConsoleNr = 4<<8 | 1        // /dev/tty1
	ttyPtsMajor  = 136
	commMax      = 15 // TASK_COMM_LEN less the NUL
	blockedAfter = 10 * time.Millisecond
)

// waits are the system calls a thread may sleep in (Linux numbers).
var waits = map[int64]bool{
	0: true, 7: true, 23: true, 34: true, 35: true, 43: true, 45: true, 47: true, 61: true,
	130: true, 202: true, 230: true, 232: true, 247: true, 270: true, 271: true, 281: true, 288: true,
}

// running says whether t is on a CPU or ready for one.
func (t *Thread) running(now int64) bool {
	nr := t.inNr.Load()
	return nr < 0 || !waits[nr] || time.Duration(now-t.inSince.Load()) < blockedAfter
}

var _ procfs.Source = (*System)(nil)

// Pids lists the process table in pid order.
func (s *System) Pids() []int {
	s.mu.Lock()
	pids := make([]int, 0, len(s.byPID))
	for pid := range s.byPID {
		pids = append(pids, pid)
	}
	s.mu.Unlock()
	sort.Ints(pids)
	return pids
}

// Process is a snapshot of pid for /proc.
func (s *System) Process(pid int) (procfs.Proc, bool) {
	s.mu.Lock()
	p, ok := s.byPID[pid]
	var out procfs.Proc
	if ok {
		out = procfs.Proc{PID: p.PID, PPID: p.PPID, PGID: p.PGID, SID: p.SID, State: 'S', Kernel: p == s.init}
		if p.zombie {
			out.State = 'Z'
		}
	}
	s.mu.Unlock()
	if !ok {
		return procfs.Proc{}, false
	}
	p.mu.Lock()
	out.Comm = comm(p.Path)
	out.Cmdline, out.Environ = p.argv, p.envp
	out.Start = p.start
	out.UID, out.GID = p.cred.UID, p.cred.GID
	out.Groups = append([]uint32(nil), p.cred.Groups...)
	out.Umask = p.umask
	out.BrkStart, out.Brk = p.brkStart, p.brk
	out.Threads, out.SigPending = len(p.threads), p.pending
	var running bool
	out.Tasks, running = p.tasksLocked()
	if running && out.State == 'S' && !out.Kernel {
		out.State = 'R'
	}
	if p.main != nil {
		out.SigBlocked = p.main.sigMask
		out.SigPending |= p.main.pending
	}
	out.SigIgnored, out.SigCaught = p.sigSets()
	mem, ctty, cwd, brkStart, brk := p.mem, p.ctty, p.cwd, p.brkStart, p.brk
	p.mu.Unlock()
	if !out.Kernel {
		out.Exe, out.Cwd = p.Path, cwd
	}
	if mem != nil && out.State != 'Z' {
		out.SizeBytes, out.RSSBytes = mem.usage()
		out.Maps = mem.maps(out.Exe, brkStart, brk)
	}
	if out.State != 'Z' {
		out.Files = p.fds.snapshot()
	}
	out.TTY, out.TPGID = s.ttyOf(ctty)
	own := p.cpu()
	kids, _ := p.childTimes()
	out.User, out.Sys, out.ChildUser, out.ChildSys = own.user, own.sys, kids.user, kids.sys
	return out, true
}

// tasksLocked is p's threads in tid order with their state and CPU time,
// and whether any is running (p.mu held).
func (p *Process) tasksLocked() ([]procfs.Task, bool) {
	now, running := time.Now().UnixNano(), false
	tasks := make([]procfs.Task, 0, len(p.threads))
	for _, t := range p.threads {
		task := procfs.Task{TID: t.TID, State: 'S'}
		if t.running(now) {
			task.State, running = 'R', true
		}
		c := t.times()
		task.User, task.Sys = c.user, c.sys
		tasks = append(tasks, task)
	}
	sort.Slice(tasks, func(i, j int) bool { return tasks[i].TID < tasks[j].TID })
	return tasks, running
}

// CPUs is every online CPU's time, as the nucleus counts it.
func (s *System) CPUs() []procfs.CPUTime {
	var out []procfs.CPUTime
	for _, c := range s.nu.CPUTimes() {
		out = append(out, procfs.CPUTime{User: c.User, Sys: c.Sys, Idle: c.Idle, Switches: c.Switches})
	}
	return out
}

// comm is a program's name as the kernel keeps it: the last part, 15 bytes.
func comm(file string) string {
	name := path.Base(file)
	if len(name) > commMax {
		name = name[:commMax]
	}
	return name
}

// sigSets are the signals p ignores and catches, as status's bit masks (p.mu held).
func (p *Process) sigSets() (ignored, caught uint64) {
	for sig := 1; sig <= nSig; sig++ {
		switch h := p.actions[sig].handler; {
		case h == sigIGN:
			ignored |= 1 << (sig - 1)
		case h != sigDFL:
			caught |= 1 << (sig - 1)
		}
	}
	return ignored, caught
}

// ttyOf is the device number of a controlling terminal (nil: the console)
// and its foreground group.
func (s *System) ttyOf(t *tty.TTY) (nr, fg int) {
	s.mu.Lock()
	console := s.tty
	nr = ttyConsoleNr
	if t != nil && t != console {
		for n, m := range s.ptys {
			if m.Slave() == t {
				nr = ttyPtsMajor<<8 | n&0xFF | (n>>8)<<20
			}
		}
	} else {
		t = console
	}
	s.mu.Unlock()
	if t == nil {
		return nr, -1
	}
	return nr, t.Foreground()
}

// usage is the address space reserved and the bytes of memory present.
func (m *Mem) usage() (size, rss uint64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, a := range m.areas {
		size += a.end - a.start
	}
	for va, pg := range m.pages {
		if _, in := m.areaAt(va); !in {
			size += PageSize
		}
		if !pg.device {
			rss += PageSize
		}
	}
	return size, rss
}

// Uptime is the time since boot.
func (s *System) Uptime() time.Duration { return time.Since(bootTime) }

// Load is the load averages and the threads running now.
func (s *System) Load() procfs.Load {
	s.mu.Lock()
	defer s.mu.Unlock()
	running, threads := s.runningLocked()
	return procfs.Load{Avg: s.loadAvg, Running: running, Threads: threads, LastPID: s.nextPID - 1}
}

// runningLocked counts the threads running, and all of them (s.mu held).
func (s *System) runningLocked() (running, threads int) {
	now := time.Now().UnixNano()
	for _, t := range s.byTID {
		threads++
		if t.running(now) {
			running++
		}
	}
	return running, threads
}

// sampleLoad folds the running count into the 1, 5 and 15 minute averages
// every loadSample, forever.
func (s *System) sampleLoad() {
	var decay [3]float64
	for i, minutes := range []float64{1, 5, 15} {
		decay[i] = math.Exp(-loadSample.Seconds() / (60 * minutes))
	}
	for range time.Tick(loadSample) {
		s.mu.Lock()
		n, _ := s.runningLocked()
		for i := range s.loadAvg {
			s.loadAvg[i] = s.loadAvg[i]*decay[i] + float64(n)*(1-decay[i])
		}
		s.mu.Unlock()
	}
}

// Memory is RAM as the nucleus counts it.
func (s *System) Memory() procfs.Memory {
	total, free := s.nu.MemInfo()
	return procfs.Memory{Total: total, Free: free}
}

// Uname is what uname(2) reports.
func (s *System) Uname() procfs.Uname {
	node, domain := s.host()
	return procfs.Uname{Sys: unameSys, Node: node, Release: unameRelease, Version: unameVersion, Domain: domain}
}
