package proc

import (
	"crypto/rand"
	"fmt"
	"io"
	"io/fs"
	"path"
	"runtime"
	"strings"
	"sync"
	"time"

	"burrow/kernel/klog"
	"burrow/kernel/tty"
	"burrow/kernel/vt"

	"gvisor.dev/gvisor/pkg/tcpip/stack"
)

// System is the set of processes and the one dispatcher that serves their
// system calls: every request from the nucleus queue is handled in its own
// goroutine (a slow read or a wait4 never holds up another process).
type System struct {
	nu      Nucleus
	files   fs.FS
	console io.Writer
	log     *klog.Logger
	names   sockNames // bound Unix socket names (socknames.go)

	mu                   sync.Mutex
	byTID                map[uint64]*Thread  // live threads, by nucleus thread
	gone                 map[uint64]bool     // threads ended while a request of theirs may still be queued
	byPID                map[int]*Process    // live processes and zombies
	nextPID              int                 // pids and tids share one number space
	reported             map[uint64]bool     // unknown syscall numbers already logged
	tty                  *tty.TTY            // the controlling terminal (SetTerminal), or nil
	ptys                 map[int]*tty.Master // open pseudo-terminal pairs by number (ptycalls.go)
	ptyAttrs             map[int]*ptyAttr    // their slaves' owners and modes (ptyowner.go)
	net                  *stack.Stack        // TCP/IP for inet sockets (W17-S03; nil: none)
	hostname, domainname string              // uname's names (hostname.go; "" until set)
	shmNames             shmTab              // /dev/shm (shm.go, W18-S03)
	sysv                 sysvTab             // SysV shared memory segments (sysv.go, W18-S04)
	sems                 semTab              // SysV semaphores (sem.go, W23-S06)
	vt                   *vt.VT              // its keyboard and display modes (SetVT), or nil
	init                 *Process            // pid 1 (kernel-side, no thread)
	loadAvg              [3]float64          // 1, 5 and 15 minutes (procsource.go, W23-S01)

	futexMu sync.Mutex
	futexes map[futexKey][]*futexWaiter // futex.go
}

// NewSystem: programs are read from files; fds 0-2 are the console.
// PID 1 is init, kept by the kernel itself (no thread): it adopts orphans
// and reaps every child of its own as soon as it exits.
func NewSystem(nu Nucleus, files fs.FS, console io.Writer, log *klog.Logger) *System {
	s := &System{nu: nu, files: files, console: console, log: log,
		byTID: map[uint64]*Thread{}, gone: map[uint64]bool{}, byPID: map[int]*Process{}, nextPID: 1, reported: map[uint64]bool{},
		futexes: map[futexKey][]*futexWaiter{}}
	s.init = s.newProcess("init", nil, &fdTable{fds: map[int]fd{}})
	s.init.PGID, s.init.SID = 1, 1
	s.init.argv = []string{"init"}
	s.byPID[1] = s.init
	go s.reap()
	return s
}

// reap is init's loop: wait for children forever, never keeping a zombie.
func (s *System) reap() {
	for range s.init.childEvent {
		s.mu.Lock()
		for pid, c := range s.init.children {
			if c.zombie {
				delete(s.init.children, pid)
				delete(s.byPID, pid)
			}
		}
		s.mu.Unlock()
	}
}

// Info is one row of the process table (the console's ps).
type Info struct {
	PID, PPID, PGID int
	State, Path     string
}

// Processes lists the process table in pid order.
func (s *System) Processes() []Info {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Info, 0, len(s.byPID))
	for pid := 1; len(out) < len(s.byPID) && pid < s.nextPID; pid++ {
		p, ok := s.byPID[pid]
		if !ok {
			continue
		}
		state := "running"
		switch {
		case p == s.init:
			state = "kernel"
		case p.zombie:
			state = "zombie"
		}
		out = append(out, Info{p.PID, p.PPID, p.PGID, state, p.Path})
	}
	return out
}

// The mmap area grows up from here (MEMORY_MAP.md: user mmap space).
const mmapBase = 0x1000_0000_0000

// image is a program loaded into a fresh Mem, ready to start.
type image struct {
	mem       *Mem
	entry, sp uint64
	end       uint64
}

// prepare loads the ELF in data with its entry stack into a new Mem; id
// goes into the auxiliary vector.
func (s *System) prepare(file string, prog io.ReaderAt, argv, envp []string, id AuxIDs) (*image, error) {
	mem, err := NewMem(s.nu)
	if err != nil {
		return nil, err
	}
	img, err := LoadELF(mem, prog)
	if err == nil && img.Interp != "" { // dynamic: the loader starts first (W18-S05)
		err = s.loadInterp(mem, &img)
	}
	if err == nil {
		err = mem.MapAnon(StackTop-StackSize, StackSize, ProtWrite)
	}
	var sp uint64
	if err == nil {
		var rnd [16]byte
		rand.Read(rnd[:])
		if len(argv) == 0 {
			argv = []string{file}
		}
		sp, err = BuildStack(mem, img, argv, envp, rnd, id)
	}
	if err != nil {
		mem.Close()
		return nil, err
	}
	start := img.Entry
	if img.Start != 0 {
		start = img.Start
	}
	return &image{mem: mem, entry: start, sp: sp, end: img.End}, nil
}

// Spawn loads the program at file (an io/fs path) and starts it as a
// child of init (which reaps it); Process.Wait reports how it ended.
func (s *System) Spawn(file string, argv, envp []string) (*Process, error) {
	prog, _, err := s.openProgram(file)
	if err != nil {
		return nil, err
	}
	im, err := s.prepare("/"+file, prog, argv, envp, AuxIDs{}) // root
	if err != nil {
		return nil, err
	}
	p := s.newProcess("/"+file, im.mem, s.stdio())
	p.brkStart, p.brk = im.end, im.end
	if len(argv) == 0 {
		argv = []string{"/" + file}
	}
	p.argv, p.envp = argv, envp
	p.PGID, p.SID, p.PPID = p.PID, p.PID, 1
	s.mu.Lock() // registered before it can make its first call
	defer s.mu.Unlock()
	s.init.children[p.PID] = p
	tid, err := s.nu.StartThread(im.mem.Space(), im.entry, im.sp, 0)
	if err != nil {
		delete(s.init.children, p.PID)
		im.mem.Close()
		return nil, err
	}
	s.register(p, tid)
	return p, nil
}

func (s *System) newProcess(file string, mem *Mem, fds *fdTable) *Process {
	return &Process{PID: s.newID(), Path: file, sys: s, mem: mem, fds: fds, mmapNext: mmapBase,
		cwd: "/", umask: defaultUmask, children: map[int]*Process{}, childEvent: make(chan struct{}, 1), done: make(chan struct{}),
		threads: map[int]*Thread{}, start: time.Since(bootTime)}
}

// newID hands out the next pid or tid.
func (s *System) newID() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	id := s.nextPID
	s.nextPID++
	return id
}

// register publishes p with its main thread, nucleus thread nt (s.mu held).
func (s *System) register(p *Process, nt uint64) *Thread {
	s.byPID[p.PID] = p
	return p.addThread(nt, p.PID)
}

// Serve runs the dispatcher until the nucleus refuses to wait.
func (s *System) Serve() error {
	go s.sampleLoad() // /proc/loadavg and sysinfo's loads (W23-S01)
	buf := make([]Request, 16)
	for {
		n, err := s.nu.Wait(buf)
		if err != nil {
			return err
		}
		for _, r := range buf[:n] {
			go s.handle(r)
		}
		// Let the handlers run before blocking again (BUG-0014): Wait is a
		// blocking syscall that keeps this M's P, so with one P a handler
		// queued now would wait for sysmon to take the P back (2 ms).
		runtime.Gosched()
	}
}

func (s *System) handle(r Request) {
	s.mu.Lock()
	t, late := s.byTID[r.TID], s.gone[r.TID]
	s.mu.Unlock()
	if t == nil && late {
		// Asked just before its process (or the thread itself) ended: drop it.
		// Whoever marked it gone (exit, execve, its own exit) kills it; a second
		// Kill here raced that one in the nucleus and hung it (BUG-0023).
		return
	}
	if t == nil {
		s.log.Errorf("request from unknown thread %d (nr %#x)", r.TID, r.Nr)
		s.nu.Kill(r.TID)
		return
	}
	switch {
	case r.Nr == InterruptNr: // stopped so that a pending signal can run
		t.stopped(false, 0, sigInfo{}, 0)
		return
	case r.IsFault() && r.Vector() == 14 && (r.Args[2]&pfPresent == 0 || r.Args[2]&pfWrite != 0):
		// not present: a first touch; present and written: maybe a
		// copy-on-write page (W18-S01). FaultIn tells which.
		switch t.p.mem.FaultIn(r.Args[0], r.Args[2]&pfWrite != 0) {
		case FaultResume:
			s.nu.Reply(t.nt, 0) // a first touch of demand-zero memory: resume
			return
		case FaultNoMem: // as Linux's OOM killer would, but only the one asking
			t.p.exit(0, sigKILL, fmt.Sprintf("out of memory at %#x (rip %#x)", r.Args[0], r.Args[1]))
			return
		}
		sig, what := faultSignal(r.Vector())
		t.stopped(true, sig, sigInfo{signo: int32(sig), code: siKernel, addr: r.Args[0], what: what}, r.Args[1])
		return
	case r.IsFault():
		sig, what := faultSignal(r.Vector())
		t.stopped(true, sig, sigInfo{signo: int32(sig), code: siKernel, addr: r.Args[0], what: what}, r.Args[1])
		return
	}
	t.enter(r.Nr)
	res, exited := t.syscall(r.Nr, r.Args)
	t.leave()
	if !exited {
		t.finish(res, r.Nr) // a pending signal runs before the thread returns
	}
}

// Page fault error code bits (args[2] of a #PF request).
const (
	pfPresent = 1 << 0 // the page was there: a protection fault
	pfWrite   = 1 << 1
)

// faultSignal maps an exception vector to the signal Linux would deliver.
func faultSignal(vector uint64) (int, string) {
	switch vector {
	case 0:
		return sigFPE, "divide error"
	case 6:
		return sigILL, "invalid opcode"
	case 14:
		return sigSEGV, "page fault"
	case 13:
		return sigSEGV, "general protection fault"
	}
	return sigSEGV, fmt.Sprintf("exception %d", vector)
}

// unknown logs a syscall number once per boot.
func (s *System) unknown(p *Process, nr uint64) {
	s.mu.Lock()
	first := !s.reported[nr]
	s.reported[nr] = true
	s.mu.Unlock()
	if first {
		s.log.Warnf("pid %d (%s): unimplemented syscall %d", p.PID, path.Base(p.Path), nr)
	}
}

// loadInterp loads img's interpreter (an absolute path) into mem.
func (s *System) loadInterp(mem *Mem, img *Image) error {
	if !strings.HasPrefix(img.Interp, "/") {
		return fmt.Errorf("elf: interpreter %q is not an absolute path", img.Interp)
	}
	r, _, err := s.openProgram(img.Interp[1:])
	if err != nil {
		return fmt.Errorf("elf: interpreter %s: %w", img.Interp, err)
	}
	img.Base, img.Start, err = LoadInterp(mem, r)
	return err
}
