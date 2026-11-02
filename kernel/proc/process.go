package proc

import (
	"io/fs"
	"path"
	"sync"
	"syscall"
)

// Signal numbers used before W10-S04 brings the rest.
const (
	sigILL  = 4
	sigFPE  = 8
	sigSEGV = 11
	sigCHLD = 17
)

// Process is one program: a thread in the nucleus, memory, descriptors,
// and its place in the process tree.
type Process struct {
	PID, PPID, PGID, SID int
	Path                 string
	sys                  *System
	mem                  *Mem
	tid                  uint64
	fds                  *fdTable

	mu            sync.Mutex
	brkStart, brk uint64
	mmapNext      uint64
	fs            uint64
	cwd           string

	children   map[int]*Process // live and zombie (s.mu)
	childEvent chan struct{}    // a child exited (capacity 1)
	zombie     bool             // exited, waiting to be reaped (s.mu)
	exitCode   int
	termSig    int
	reason     string
	done       chan struct{}
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

// waitStatus is Linux's wait4 status word.
func (p *Process) waitStatus() uint64 {
	if p.termSig != 0 {
		return uint64(p.termSig & 0x7F)
	}
	return uint64(p.exitCode&0xFF) << 8
}

// Mem is the process's memory (tests use it to stage syscall arguments).
func (p *Process) Mem() *Mem { return p.mem }

// TID is the nucleus thread.
func (p *Process) TID() uint64 { return p.tid }

// exit ends the process: thread, memory, descriptors. With a parent that
// can wait for it, it stays a zombie until reaped; its children are
// orphaned to the kernel (an init process adopts them in W10-S07).
func (p *Process) exit(code, sig int, reason string) {
	s := p.sys
	if err := s.nu.Kill(p.tid); err != nil {
		s.log.Errorf("pid %d: kill: %v", p.PID, err)
	}
	if err := p.mem.Close(); err != nil {
		s.log.Errorf("pid %d: free memory: %v", p.PID, err)
	}
	p.fds.closeAll()
	if reason != "" {
		s.log.Warnf("pid %d (%s) killed: %s", p.PID, path.Base(p.Path), reason)
	}
	s.mu.Lock()
	p.exitCode, p.termSig, p.reason = code, sig, reason
	delete(s.byTID, p.tid)
	for pid, c := range p.children {
		c.PPID = 0
		if c.zombie {
			delete(s.byPID, pid)
		}
	}
	p.children = nil
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
}

// fork: a copy of p (memory, descriptors, registers with RAX = 0) as its child.
func (p *Process) fork() int64 {
	s := p.sys
	var regs Regs
	if err := s.nu.Regs(p.tid, &regs, false); err != nil {
		return errno(err)
	}
	mem, err := p.mem.Clone()
	if err != nil {
		return errno(err)
	}
	c := s.newProcess(p.Path, mem, p.fds.clone())
	p.mu.Lock()
	c.brkStart, c.brk, c.mmapNext, c.fs, c.cwd = p.brkStart, p.brk, p.mmapNext, p.fs, p.cwd
	p.mu.Unlock()
	c.PPID, c.PGID, c.SID = p.PID, p.PGID, p.SID
	regs.RAX = 0
	s.mu.Lock()
	defer s.mu.Unlock()
	tid, err := s.nu.StartThreadFrom(mem.Space(), &regs)
	if err != nil {
		mem.Close()
		c.fds.closeAll()
		return errno(err)
	}
	s.register(c, tid)
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
func (p *Process) wait4(pid int64, statusVA, options uint64) int64 {
	s := p.sys
	for {
		s.mu.Lock()
		var found *Process
		any := false
		for _, c := range p.children {
			if pid > 0 && int64(c.PID) != pid || pid == 0 && c.PGID != p.PGID || pid < -1 && int64(c.PGID) != -pid {
				continue
			}
			any = true
			if c.zombie {
				found = c
				break
			}
		}
		if found != nil {
			delete(p.children, found.PID)
			delete(s.byPID, found.PID)
		}
		s.mu.Unlock()
		switch {
		case found != nil:
			if statusVA != 0 {
				var b [4]byte
				st := found.waitStatus()
				b[0], b[1] = byte(st), byte(st>>8)
				if err := p.mem.CopyOut(statusVA, b[:]); err != nil {
					return errno(err)
				}
			}
			return int64(found.PID)
		case !any:
			return -int64(syscall.ECHILD)
		case options&wNoHang != 0:
			return 0
		}
		<-p.childEvent
	}
}

// execve: a new program in place of this one (same pid, descriptors stay
// open). Everything that can fail happens before the old image goes.
func (p *Process) execve(pathVA, argvVA, envpVA uint64) int64 {
	s := p.sys
	name, err := p.mem.CString(pathVA, 4096)
	if err != nil {
		return errno(err)
	}
	argv, err := p.strings(argvVA)
	if err != nil {
		return errno(err)
	}
	envp, err := p.strings(envpVA)
	if err != nil {
		return errno(err)
	}
	file := p.resolve(name)
	data, err := fs.ReadFile(s.files, file)
	if err != nil {
		return errno(err)
	}
	im, err := s.prepare("/"+file, data, argv, envp)
	if err != nil {
		return -int64(syscall.ENOEXEC)
	}
	if err := s.nu.SetSpace(p.tid, im.mem.Space()); err != nil {
		im.mem.Close()
		return errno(err)
	}
	fresh := FreshRegs(im.entry, im.sp)
	if err := s.nu.Regs(p.tid, &fresh, true); err != nil {
		s.log.Errorf("pid %d: execve registers: %v", p.PID, err)
	}
	old := p.mem
	p.mu.Lock()
	p.mem, p.Path, p.fs = im.mem, "/"+file, 0
	p.brkStart, p.brk, p.mmapNext = im.end, im.end, mmapBase
	p.mu.Unlock()
	p.fds.closeOnExec()
	old.Close()
	return 0 // RAX of the new program's first instruction
}

// strings reads a NULL-terminated array of string pointers (argv, envp).
func (p *Process) strings(va uint64) ([]string, error) {
	var out []string
	total := 0
	for i := 0; va != 0 && i < maxExecArgs; i++ {
		var b [8]byte
		if err := p.mem.CopyIn(va+uint64(8*i), b[:]); err != nil {
			return nil, err
		}
		ptr := uint64(b[0]) | uint64(b[1])<<8 | uint64(b[2])<<16 | uint64(b[3])<<24 |
			uint64(b[4])<<32 | uint64(b[5])<<40 | uint64(b[6])<<48 | uint64(b[7])<<56
		if ptr == 0 {
			return out, nil
		}
		str, err := p.mem.CString(ptr, maxExecSize-total)
		if err != nil {
			return nil, syscall.E2BIG
		}
		total += len(str) + 1
		out = append(out, str)
	}
	if va == 0 {
		return nil, nil
	}
	return nil, syscall.E2BIG
}
