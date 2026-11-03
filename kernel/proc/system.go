package proc

import (
	"crypto/rand"
	"fmt"
	"io"
	"io/fs"
	"path"
	"sync"

	"burrow/kernel/klog"
	"burrow/kernel/tty"
)

// System is the set of processes and the one dispatcher that serves their
// system calls: every request from the nucleus queue is handled in its own
// goroutine (a slow read or a wait4 never holds up another process).
type System struct {
	nu      Nucleus
	files   fs.FS
	console io.Writer
	log     *klog.Logger

	mu       sync.Mutex
	byTID    map[uint64]*Process // live processes, by their (one) thread
	byPID    map[int]*Process    // live processes and zombies
	nextPID  int
	reported map[uint64]bool // unknown syscall numbers already logged
	tty      *tty.TTY        // the controlling terminal (SetTerminal), or nil
}

// NewSystem: programs are read from files; fds 0-2 are the console.
func NewSystem(nu Nucleus, files fs.FS, console io.Writer, log *klog.Logger) *System {
	return &System{nu: nu, files: files, console: console, log: log,
		byTID: map[uint64]*Process{}, byPID: map[int]*Process{}, nextPID: 1, reported: map[uint64]bool{}}
}

// The mmap area grows up from here (MEMORY_MAP.md: user mmap space).
const mmapBase = 0x1000_0000_0000

// image is a program loaded into a fresh Mem, ready to start.
type image struct {
	mem       *Mem
	entry, sp uint64
	end       uint64
}

// prepare loads the ELF in data with its entry stack into a new Mem.
func (s *System) prepare(file string, data []byte, argv, envp []string) (*image, error) {
	mem, err := NewMem(s.nu)
	if err != nil {
		return nil, err
	}
	img, err := LoadELF(mem, bytesReaderAt(data))
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
		sp, err = BuildStack(mem, img, argv, envp, rnd)
	}
	if err != nil {
		mem.Close()
		return nil, err
	}
	return &image{mem: mem, entry: img.Entry, sp: sp, end: img.End}, nil
}

// Spawn loads the program at file (an io/fs path) and starts it as a
// child of the kernel (ppid 0): nobody waits for it but Process.Wait.
func (s *System) Spawn(file string, argv, envp []string) (*Process, error) {
	data, err := fs.ReadFile(s.files, file)
	if err != nil {
		return nil, err
	}
	im, err := s.prepare("/"+file, data, argv, envp)
	if err != nil {
		return nil, err
	}
	p := s.newProcess("/"+file, im.mem, s.stdio())
	p.brkStart, p.brk = im.end, im.end
	p.PGID, p.SID = p.PID, p.PID
	s.mu.Lock() // registered before it can make its first call
	defer s.mu.Unlock()
	tid, err := s.nu.StartThread(im.mem.Space(), im.entry, im.sp, 0)
	if err != nil {
		im.mem.Close()
		return nil, err
	}
	s.register(p, tid)
	return p, nil
}

func (s *System) newProcess(file string, mem *Mem, fds *fdTable) *Process {
	s.mu.Lock()
	pid := s.nextPID
	s.nextPID++
	s.mu.Unlock()
	return &Process{PID: pid, Path: file, sys: s, mem: mem, fds: fds, mmapNext: mmapBase,
		cwd: "/", children: map[int]*Process{}, childEvent: make(chan struct{}, 1), done: make(chan struct{}),
		sigWake: make(chan struct{})}
}

// register publishes p with its thread (s.mu held).
func (s *System) register(p *Process, tid uint64) {
	p.tid = tid
	s.byTID[tid] = p
	s.byPID[p.PID] = p
}

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
	p := s.byTID[r.TID]
	s.mu.Unlock()
	if p == nil {
		s.log.Errorf("request from unknown thread %d (nr %#x)", r.TID, r.Nr)
		s.nu.Kill(r.TID)
		return
	}
	switch {
	case r.Nr == InterruptNr: // stopped so that a pending signal can run
		p.stopped(false, 0, sigInfo{}, 0)
		return
	case r.IsFault():
		sig, what := faultSignal(r.Vector())
		p.stopped(true, sig, sigInfo{signo: int32(sig), code: siKernel, addr: r.Args[0], what: what}, r.Args[1])
		return
	}
	res, exited := p.syscall(r.Nr, r.Args)
	if !exited {
		p.finish(res, r.Nr) // a pending signal runs before the thread returns
	}
}

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
