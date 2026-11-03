package proc

// altstack.go - sigaltstack(2) (W12-S01): each thread's alternate signal
// stack, where SA_ONSTACK handlers run (sigframe.go places the frame). The
// Go runtime refuses to handle a signal anywhere else. Never changed while
// the thread is running on it. Docs: POSIX_COVERAGE.md (Signals).

import (
	"encoding/binary"
	"syscall"
)

const (
	ssOnstack   = 1
	ssDisable   = 2
	minSigStack = 2048 // MINSIGSTKSZ
)

// sigaltstack(ss, old_ss): the calling thread's alternate signal stack,
// stack_t { void *ss_sp; int ss_flags; size_t ss_size; }.
func sysSigaltstackFn(t *Thread, a [6]uint64) (int64, bool) {
	p := t.p
	var r Regs
	if err := p.sys.nu.Regs(t.nt, &r, false); err != nil {
		return ok(errno(err))
	}
	p.mu.Lock()
	cur := altStack{t.altSP, t.altSize}
	p.mu.Unlock()
	on := cur.holds(r.RSP)
	le := binary.LittleEndian
	if a[1] != 0 {
		b := make([]byte, 24)
		le.PutUint64(b[0:], cur.sp)
		flags := uint32(0)
		switch {
		case cur.size == 0:
			flags = ssDisable
		case on:
			flags = ssOnstack
		}
		le.PutUint32(b[8:], flags)
		le.PutUint64(b[16:], cur.size)
		if err := p.mem.CopyOut(a[1], b); err != nil {
			return ok(errno(err))
		}
	}
	if a[0] == 0 {
		return ok(0)
	}
	b := make([]byte, 24)
	if err := p.mem.CopyIn(a[0], b); err != nil {
		return ok(errno(err))
	}
	sp, flags, size := le.Uint64(b[0:]), le.Uint32(b[8:]), le.Uint64(b[16:])
	switch {
	case on:
		return ok(-int64(syscall.EPERM)) // not while running on it
	case flags&^ssDisable != 0:
		return ok(-int64(syscall.EINVAL))
	case flags&ssDisable != 0:
		sp, size = 0, 0
	case size < minSigStack:
		return ok(-int64(syscall.ENOMEM))
	}
	p.mu.Lock()
	t.altSP, t.altSize = sp, size
	p.mu.Unlock()
	return ok(0)
}
