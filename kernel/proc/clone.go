package proc

// clone.go - clone(2) (W12-S01): the fork shape (a new process) and the
// thread shape (a new thread in this process, as pthread_create and the Go
// runtime ask). A new thread is registered before it can run, so its first
// system call always finds it. Docs: docs/04-plan/waves/WAVE-12-ports.md.

import (
	"encoding/binary"
	"syscall"
)

const (
	cloneVM            = 0x100
	cloneFS            = 0x200
	cloneFiles         = 0x400
	cloneSighand       = 0x800
	cloneVfork         = 0x4000
	cloneThread        = 0x10000
	cloneSettls        = 0x80000
	cloneParentSettid  = 0x100000
	cloneChildCleartid = 0x200000
	cloneChildSettid   = 0x1000000
	exitSignal         = 0xFF // low byte: the signal sent to the parent at exit

	threadNeeds = cloneVM | cloneSighand | cloneThread
)

// clone(flags, stack, parent_tid, child_tid, tls) on x86-64.
func sysCloneFn(t *Thread, a [6]uint64) (int64, bool) {
	flags := a[0]
	switch {
	case flags&cloneThread == 0: // a new process: fork (CLONE_VM|CLONE_VFORK copies too)
		return ok(t.fork())
	case flags&threadNeeds != threadNeeds || flags&(cloneFS|cloneFiles) != cloneFS|cloneFiles:
		return ok(-int64(syscall.EINVAL)) // a thread shares memory, handlers, files and cwd
	}
	return ok(t.cloneThread(flags, a[1], a[2], a[3], a[4]))
}

// cloneThread starts a thread of t's process from t's registers: RAX 0, the
// given stack (0: the same), TLS if CLONE_SETTLS. It returns the new TID.
func (t *Thread) cloneThread(flags, stack, parentTID, childTID, tls uint64) int64 {
	p, s := t.p, t.p.sys
	var regs Regs
	if err := s.nu.Regs(t.nt, &regs, false); err != nil {
		return errno(err)
	}
	regs.RAX = 0
	if stack != 0 {
		regs.RSP = stack
	}
	p.mu.Lock()
	fs, mask := t.fs, t.sigMask
	p.mu.Unlock()
	if flags&cloneSettls != 0 {
		fs = tls
	}
	regs.FSBase = fs
	tid := s.newID()
	var word [4]byte
	binary.LittleEndian.PutUint32(word[:], uint32(tid))
	for _, w := range []struct {
		flag, va uint64
	}{{cloneParentSettid, parentTID}, {cloneChildSettid, childTID}} {
		if flags&w.flag != 0 {
			p.mem.CopyOut(w.va, word[:]) // a bad address is ignored, as Linux does
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	nt, err := s.nu.StartThreadFrom(p.mem.Space(), &regs)
	if err != nil {
		return errno(err)
	}
	c := p.addThread(nt, tid)
	p.mu.Lock()
	c.sigMask, c.fs = mask, fs // the alternate signal stack is not inherited (Linux)
	if flags&cloneChildCleartid != 0 {
		c.clearTID = childTID
	}
	p.mu.Unlock()
	return int64(tid)
}
