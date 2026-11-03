package proc

import (
	"io"
	"syscall"

	"burrow/kernel/fs/devfs"
)

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
		if err := p.mem.Reserve(oldTop, newTop-oldTop, ProtWrite, false); err != nil {
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
	mapPrivate  = 0x02
	mapFixed    = 0x10
	mapAnon     = 0x20
	mmapMaxSize = 1 << 46 // reservations are free (area.go): the Go runtime asks for hundreds of MiB
)

// mmap: anonymous private memory (W9), device memory with MAP_SHARED
// (/dev/fb0, W11) and private mappings of regular files (W14-S05).
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
		f := p.fds.get(a[4])
		if f == nil {
			return ok(-int64(syscall.EBADF))
		}
		if _, isDev := device(f).(Mappable); isDev {
			return ok(p.mmapDevice(a[4], a[5], n, flags, pr, flags&mapFixed != 0, addr))
		}
		return ok(p.mmapFile(f, a[5], n, flags, prot, pr, addr))
	}
	addr, e := p.pickAddr(addr, n, flags&mapFixed != 0)
	if e != 0 {
		return ok(e)
	}
	if err := p.mem.Reserve(addr, n, pr, prot&7 == 0); err != nil {
		return ok(-int64(syscall.ENOMEM))
	}
	return ok(int64(addr))
}

// mmapFile maps n bytes of a regular file from offset off, privately:
// pages fill from the file at first touch, writes stay in the process.
// A read-only MAP_SHARED mapping is the same thing; a writable one would
// need writes to reach the file, so it is -ENODEV.
func (p *Process) mmapFile(f *file, off, n, flags, prot uint64, pr Prot, addr uint64) int64 {
	r, isR := f.f.(io.ReaderAt)
	if _, isDev := f.f.(interface{ Device() devfs.Device }); !isR || isDev {
		return -int64(syscall.ENODEV) // pipes, the terminal, devices without memory
	}
	if st, err := f.f.Stat(); err != nil || st.IsDir() {
		return -int64(syscall.ENODEV)
	}
	switch {
	case off%PageSize != 0 || int64(off) < 0 || flags&(mapShared|mapPrivate) == 0:
		return -int64(syscall.EINVAL)
	case f.flags&oAccMode == oWRONLY:
		return -int64(syscall.EACCES)
	case flags&mapShared != 0 && prot&protWrite != 0:
		return -int64(syscall.ENODEV)
	}
	addr, e := p.pickAddr(addr, n, flags&mapFixed != 0)
	if e != 0 {
		return e
	}
	if err := p.mem.ReserveFile(addr, n, pr, prot&7 == 0, r, int64(off)); err != nil {
		return -int64(syscall.ENOMEM)
	}
	return int64(addr)
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

// mprotect: new protection for mapped memory (musl opens a thread's stack
// with it). PROT_NONE makes untouched pages inaccessible; touched ones stay
// readable.
func sysMprotectFn(p *Process, a [6]uint64) (int64, bool) {
	if a[0]%PageSize != 0 {
		return ok(-int64(syscall.EINVAL))
	}
	var pr Prot
	if a[2]&protWrite != 0 {
		pr |= ProtWrite
	}
	if a[2]&protExec != 0 {
		pr |= ProtExec
	}
	switch err := p.mem.Protect(a[0], pageUp(a[1]), pr, a[2]&7 == 0); {
	case err == errNotMapped:
		return ok(-int64(syscall.ENOMEM))
	case err != nil:
		return ok(-int64(syscall.EINVAL))
	}
	return ok(0)
}

func init() { table[10] = sysMprotectFn }

func sysMunmapFn(p *Process, a [6]uint64) (int64, bool) {
	if a[0]%PageSize != 0 || a[0] >= UserTop || a[1] == 0 {
		return ok(-int64(syscall.EINVAL))
	}
	return ok(errno(p.mem.Unmap(a[0], pageUp(a[1]))))
}
