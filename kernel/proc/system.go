package proc

import (
	"crypto/rand"
	"fmt"
	"io"
	"io/fs"
	"path"
	"sync"

	"burrow/kernel/klog"
)

// System is the set of processes and the one dispatcher that serves their
// system calls: every request from the nucleus queue is handled in its own
// goroutine (a slow read never holds up another process).
type System struct {
	nu      Nucleus
	files   fs.FS
	console io.Writer
	log     *klog.Logger

	mu       sync.Mutex
	procs    map[uint64]*Process // by tid (one thread per process in W9)
	nextPID  int
	reported map[uint64]bool // unknown syscall numbers already logged
}

// NewSystem: programs are read from files; fds 0-2 are the console.
func NewSystem(nu Nucleus, files fs.FS, console io.Writer, log *klog.Logger) *System {
	return &System{nu: nu, files: files, console: console, log: log,
		procs: map[uint64]*Process{}, nextPID: 1, reported: map[uint64]bool{}}
}

// Process is one running program.
type Process struct {
	PID  int
	Path string
	sys  *System
	mem  *Mem
	tid  uint64
	fds  *fdTable

	mu            sync.Mutex
	brkStart, brk uint64
	mmapNext      uint64
	fs            uint64
	done          chan struct{}
	status        int
	reason        string
}

// The mmap area grows up from here (MEMORY_MAP.md: user mmap space).
const mmapBase = 0x1000_0000_0000

// Spawn loads the program at path (an io/fs path) and starts it.
func (s *System) Spawn(file string, argv, envp []string) (*Process, error) {
	data, err := fs.ReadFile(s.files, file)
	if err != nil {
		return nil, err
	}
	mem, err := NewMem(s.nu)
	if err != nil {
		return nil, err
	}
	p, err := s.load(mem, "/"+file, data, argv, envp)
	if err != nil {
		mem.Close()
		return nil, err
	}
	return p, nil
}

func (s *System) load(mem *Mem, file string, data []byte, argv, envp []string) (*Process, error) {
	img, err := LoadELF(mem, bytesReaderAt(data))
	if err != nil {
		return nil, err
	}
	if err := mem.MapAnon(StackTop-StackSize, StackSize, ProtWrite); err != nil {
		return nil, err
	}
	var rnd [16]byte
	rand.Read(rnd[:])
	if len(argv) == 0 {
		argv = []string{file}
	}
	sp, err := BuildStack(mem, img, argv, envp, rnd)
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	pid := s.nextPID
	s.nextPID++
	s.mu.Unlock()
	p := &Process{PID: pid, Path: file, sys: s, mem: mem, fds: newFDTable(s.console),
		brkStart: img.End, brk: img.End, mmapNext: mmapBase, done: make(chan struct{})}
	s.mu.Lock() // registered before it can make its first call
	defer s.mu.Unlock()
	tid, err := s.nu.StartThread(mem.Space(), img.Entry, sp, 0)
	if err != nil {
		return nil, err
	}
	p.tid = tid
	s.procs[tid] = p
	return p, nil
}

// Wait blocks until the process is gone and returns its status: the exit
// code, or 128 + the signal Linux would have sent for a fault.
func (p *Process) Wait() (status int, reason string) {
	<-p.done
	return p.status, p.reason
}

// Mem is the process's memory (tests use it to stage syscall arguments).
func (p *Process) Mem() *Mem { return p.mem }

// TID is the nucleus thread.
func (p *Process) TID() uint64 { return p.tid }

// Serve runs the dispatcher until the nucleus refuses to wait.
func (s *System) Serve() error {
	buf := make([]Request, 16)
	for {
		n, err := s.nu.Wait(buf)
		if err != nil {
			return err
		}
		for _, r := range buf[:n] {
			go s.handle(r)
		}
	}
}

func (s *System) handle(r Request) {
	s.mu.Lock()
	p := s.procs[r.TID]
	s.mu.Unlock()
	if p == nil {
		s.log.Errorf("request from unknown thread %d (nr %#x)", r.TID, r.Nr)
		s.nu.Kill(r.TID)
		return
	}
	if r.IsFault() {
		sig, what := faultSignal(r.Vector())
		p.exit(128+sig, fmt.Sprintf("%s at %#x (rip %#x)", what, r.Args[0], r.Args[1]))
		return
	}
	res, exited := p.syscall(r.Nr, r.Args)
	if exited {
		return
	}
	if err := s.nu.Reply(r.TID, res); err != nil {
		s.log.Errorf("pid %d: reply: %v", p.PID, err)
	}
}

// faultSignal maps an exception vector to the signal Linux would deliver.
func faultSignal(vector uint64) (int, string) {
	switch vector {
	case 0:
		return 8, "divide error" // SIGFPE
	case 6:
		return 4, "invalid opcode" // SIGILL
	case 14:
		return 11, "page fault" // SIGSEGV
	case 13:
		return 11, "general protection fault"
	}
	return 11, fmt.Sprintf("exception %d", vector)
}

// exit ends the process: its thread, its memory, its files.
func (p *Process) exit(status int, reason string) {
	s := p.sys
	if err := s.nu.Kill(p.tid); err != nil {
		s.log.Errorf("pid %d: kill: %v", p.PID, err)
	}
	if err := p.mem.Close(); err != nil {
		s.log.Errorf("pid %d: free memory: %v", p.PID, err)
	}
	p.fds.closeAll()
	s.mu.Lock()
	delete(s.procs, p.tid)
	s.mu.Unlock()
	if reason != "" {
		s.log.Warnf("pid %d (%s) killed: %s", p.PID, path.Base(p.Path), reason)
	}
	p.status, p.reason = status, reason
	close(p.done)
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
