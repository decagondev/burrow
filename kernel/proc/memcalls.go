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
	mapFixed    = 0x10
	mapAnon     = 0x20
	mmapMaxSize = 1 << 30
)

// mmap: anonymous private memory only in W9 (files and devices: W11).
func sysMmapFn(p *Process, a [6]uint64) (int64, bool) {
	addr, n, prot, flags := a[0], a[1], a[2], a[3]
	if flags&mapAnon == 0 {
		return ok(-int64(syscall.ENODEV))
	}
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
	p.mu.Lock()
	if flags&mapFixed != 0 {
		if addr%PageSize != 0 || addr >= UserTop || n > UserTop-addr {
			p.mu.Unlock()
			return ok(-int64(syscall.EINVAL))
		}
		p.mem.Unmap(addr, n) // MAP_FIXED replaces what was there
	} else {
		for p.mem.Mapped(p.mmapNext, 1) { // skip MAP_FIXED leftovers
			p.mmapNext += PageSize
		}
		addr = p.mmapNext
		p.mmapNext += n + PageSize // one unmapped guard page between mappings
	}
	p.mu.Unlock()
	if err := p.mem.MapAnon(addr, n, pr); err != nil {
		return ok(-int64(syscall.ENOMEM))
	}
	return ok(int64(addr))
}

func sysMunmapFn(p *Process, a [6]uint64) (int64, bool) {
	if a[0]%PageSize != 0 || a[0] >= UserTop || a[1] == 0 {
		return ok(-int64(syscall.EINVAL))
	}
	return ok(errno(p.mem.Unmap(a[0], pageUp(a[1]))))
}
