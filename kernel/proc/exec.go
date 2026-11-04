package proc

// exec.go - execve(2) (W10-S02; threads since W12-S01): a new program in
// place of the calling one, same pid, descriptors kept unless close-on-exec.
// Everything that can fail happens before the old image goes; then the
// other threads end and the caller becomes the main thread. Never leaves a
// process half replaced. Docs: POSIX_COVERAGE.md.

import (
	"io/fs"
	"syscall"
)

// execve: a new program in place of this one (same pid, descriptors stay
// open). Everything that can fail happens before the old image goes; then
// every other thread ends and the caller becomes the main thread.
func (t *Thread) execve(pathVA, argvVA, envpVA uint64) int64 {
	p, s := t.p, t.p.sys
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
	if file, data, argv, err = p.script(name, file, data, argv); err != nil { // "#!" (W16-S04)
		return errno(err)
	}
	im, err := s.prepare("/"+file, data, argv, envp)
	if err != nil {
		return -int64(syscall.ENOEXEC)
	}
	t.endOthers()
	if err := s.nu.SetSpace(t.nt, im.mem.Space()); err != nil {
		im.mem.Close()
		return errno(err)
	}
	fresh := FreshRegs(im.entry, im.sp)
	if err := s.nu.Regs(t.nt, &fresh, true); err != nil {
		s.log.Errorf("pid %d: execve registers: %v", p.PID, err)
	}
	s.futexDrop(p.mem)
	old := p.mem
	p.mu.Lock()
	delete(p.threads, t.TID)
	t.TID, t.fs, t.altSP, t.altSize, t.clearTID = p.PID, 0, 0, 0, 0
	p.threads[t.TID], p.main = t, t
	p.mem, p.Path = im.mem, "/"+file
	p.brkStart, p.brk, p.mmapNext = im.end, im.end, mmapBase
	for sig := range p.actions { // handlers do not survive the old program; "ignore" does
		if p.actions[sig].handler != sigIGN {
			p.actions[sig] = sigaction{}
		}
	}
	p.mu.Unlock()
	p.fds.closeOnExec()
	old.Close()
	return 0 // RAX of the new program's first instruction
}

// endOthers ends every thread of t's process but t (execve).
func (t *Thread) endOthers() {
	p, s := t.p, t.p.sys
	p.mu.Lock()
	var others []*Thread
	for tid, o := range p.threads {
		if o != t {
			o.exited = true
			others = append(others, o)
			delete(p.threads, tid)
		}
	}
	p.mu.Unlock()
	s.mu.Lock()
	for _, o := range others {
		delete(s.byTID, o.nt)
		s.gone[o.nt] = true
	}
	s.mu.Unlock()
	for _, o := range others {
		s.nu.Kill(o.nt)
	}
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
