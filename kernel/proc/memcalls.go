package proc

import "syscall"

// brk: grow or shrink the heap that starts after the program's segments.
// Linux semantics: the result is the (new or unchanged) break, never an error.
func sysBrkFn(p *Process, a [6]uint64) (int64, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	want := a[0]
	if want < p.brkStart || want >= mmapBase {
		return ok(int64(p.brk))
	}
	oldTop, newTop := pageUp(p.brk), pageUp(want)
	switch {
	case newTop > oldTop:
		if err := p.mem.MapAnon(oldTop, newTop-oldTop, ProtWrite); err != nil {
			return ok(int64(p.brk))
		}
	case newTop < oldTop:
		p.mem.Unmap(newTop, oldTop-newTop)
	}
	p.brk = want
	return ok(int64(p.brk))
}

const (
	protWrite   = 2
	protExec    = 4
	mapShared   = 0x01
	mapFixed    = 0x10
	mapAnon     = 0x20
	mmapMaxSize = 1 << 30
)

// mmap: anonymous private memory (W9) and device memory with MAP_SHARED
// (/dev/fb0, W11); regular files are not mappable yet.
func sysMmapFn(p *Process, a [6]uint64) (int64, bool) {
	addr, n, prot, flags := a[0], a[1], a[2], a[3]
	if n == 0 || n > mmapMaxSize {
		return ok(-int64(syscall.EINVAL))
	}
	n = pageUp(n)
	var pr Prot
	if prot&protWrite != 0 {
		pr |= ProtWrite
	}
	if prot&protExec != 0 {
		pr |= ProtExec
	}
	if flags&mapAnon == 0 {
		return ok(p.mmapDevice(a[4], a[5], n, flags, pr, flags&mapFixed != 0, addr))
	}
	addr, e := p.pickAddr(addr, n, flags&mapFixed != 0)
	if e != 0 {
		return ok(e)
	}
	if err := p.mem.MapAnon(addr, n, pr); err != nil {
		return ok(-int64(syscall.ENOMEM))
	}
	return ok(int64(addr))
}

// pickAddr chooses where n bytes go: addr itself for MAP_FIXED (what was
// there is unmapped), else the next free range above mmapBase.
func (p *Process) pickAddr(addr, n uint64, fixed bool) (uint64, int64) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if fixed {
		if addr%PageSize != 0 || addr >= UserTop || n > UserTop-addr {
			return 0, -int64(syscall.EINVAL)
		}
		p.mem.Unmap(addr, n) // MAP_FIXED replaces what was there
		return addr, 0
	}
	for p.mem.Mapped(p.mmapNext, 1) { // skip MAP_FIXED leftovers
		p.mmapNext += PageSize
	}
	addr = p.mmapNext
	p.mmapNext += n + PageSize // one unmapped guard page between mappings
	return addr, 0
}

func sysMunmapFn(p *Process, a [6]uint64) (int64, bool) {
	if a[0]%PageSize != 0 || a[0] >= UserTop || a[1] == 0 {
		return ok(-int64(syscall.EINVAL))
	}
	return ok(errno(p.mem.Unmap(a[0], pageUp(a[1]))))
}
