package proc

// dump.go - what every process and thread is doing (W20-S01), for a hang:
// the console prints it when SysRq is pressed, and the smoke tests press it
// before they give up on a boot. Per process: pid, parent, group, state,
// path; per thread: the system call it is in and for how long, its mask
// and pending signals; then the stacks of the Go kernel's goroutines in
// this package (the handlers, the waits). Reads what it can without the
// locks a stuck thread may hold: try-locks, and atomics for the syscall in
// flight. Never changes anything. Docs: docs/04-plan/waves/WAVE-20-smp.md.

import (
	"bytes"
	"fmt"
	"io"
	"runtime"
	"sort"
	"time"
)

// enter and leave record the system call a thread is in.
func (t *Thread) enter(nr uint64) {
	t.inSince.Store(time.Now().UnixNano())
	t.inNr.Store(int64(nr))
}

func (t *Thread) leave() { t.inNr.Store(-1) }

// Dump writes the state of every process to w.
func (s *System) Dump(w io.Writer) {
	locked := s.mu.TryLock()
	procs := make([]*Process, 0, len(s.byPID))
	for _, p := range s.byPID {
		procs = append(procs, p)
	}
	if locked {
		s.mu.Unlock()
	}
	sort.Slice(procs, func(i, j int) bool { return procs[i].PID < procs[j].PID })
	fmt.Fprintf(w, "[dump] %d processes (system lock %s)\n", len(procs), held(!locked))
	for _, p := range procs {
		p.dump(w)
	}
	fmt.Fprintf(w, "[dump] goroutines in kernel/proc:\n")
	buf := make([]byte, 1<<20)
	buf = buf[:runtime.Stack(buf, true)]
	for _, g := range bytes.Split(buf, []byte("\n\n")) {
		if bytes.Contains(g, []byte("burrow/kernel/proc.")) {
			for _, line := range bytes.Split(g, []byte("\n")) {
				fmt.Fprintf(w, "[dump]   %s\n", line)
			}
		}
	}
	fmt.Fprintf(w, "[dump] end\n")
}

func held(b bool) string {
	if b {
		return "held"
	}
	return "free"
}

func (p *Process) dump(w io.Writer) {
	locked := p.mu.TryLock()
	threads := make([]*Thread, 0, len(p.threads))
	for _, t := range p.threads {
		threads = append(threads, t)
	}
	state := "live"
	switch {
	case p.zombie:
		state = "zombie"
	case p.exited:
		state = "exiting"
	}
	pending := p.pending
	if locked {
		p.mu.Unlock()
	}
	fmt.Fprintf(w, "[dump] pid %d ppid %d pgid %d %s %s: %d threads, pending %#x, lock %s\n",
		p.PID, p.PPID, p.PGID, state, p.Path, len(threads), pending, held(!locked))
	sort.Slice(threads, func(i, j int) bool { return threads[i].TID < threads[j].TID })
	for _, t := range threads {
		if nr := t.inNr.Load(); nr >= 0 {
			since := time.Duration(time.Now().UnixNano() - t.inSince.Load())
			fmt.Fprintf(w, "[dump]   tid %d in syscall %d for %v; mask %#x pending %#x\n",
				t.TID, nr, since.Round(time.Millisecond), t.sigMask, t.pending)
		} else {
			fmt.Fprintf(w, "[dump]   tid %d in user space (or between requests); mask %#x pending %#x\n",
				t.TID, t.sigMask, t.pending)
		}
	}
}
