package proc

// remap.go - mremap(2) (W16-S02, from the X census: musl's realloc grows
// large blocks with it). A mapping shrinks in place, grows in place when the
// range above it is free, and with MREMAP_MAYMOVE moves: its areas shift
// (file offsets kept) and its backed pages are re-mapped to the new address,
// frame by frame, so nothing is copied. MREMAP_FIXED moves to a chosen
// address. Never duplicates a mapping (old size 0 is EINVAL: there is no
// shared memory to duplicate). Docs: POSIX_COVERAGE.md.

import (
	"sort"
	"syscall"
)

const (
	mremapMaymove = 1
	mremapFixed   = 2
)

// covered: every page of [va, va+n) is backed or in an area (m.mu held).
func (m *Mem) covered(va, n uint64) bool {
	for p := va; p < va+n; p += PageSize {
		if _, ok := m.pages[p]; !ok {
			if _, inArea := m.areaAt(p); !inArea {
				return false
			}
		}
	}
	return true
}

// free: nothing at all in [va, va+n) (m.mu held).
func (m *Mem) free(va, n uint64) bool {
	if va >= UserTop || n > UserTop-va {
		return false
	}
	for p := va; p < va+n; p += PageSize {
		if _, ok := m.pages[p]; ok {
			return false
		}
		if _, inArea := m.areaAt(p); inArea {
			return false
		}
	}
	return true
}

// extend grows the mapping ending at end by n bytes, as the area holding
// its last page (same protection, and for a file the following bytes of it).
func (m *Mem) extend(end, n uint64) {
	a, ok := m.areaAt(end - 1)
	if !ok {
		a = area{prot: m.pages[end-PageSize].prot}
	}
	tail := a.slice(end, end+n) // keeps the file offset running on
	if !ok {
		tail.file = nil
	}
	m.areas = append(m.areas, tail)
	sort.Slice(m.areas, func(i, j int) bool { return m.areas[i].start < m.areas[j].start })
}

// move shifts [from, from+n) to [to, to+n): areas and backed pages (m.mu held).
func (m *Mem) move(from, to, n uint64) error {
	var moved []area
	for _, a := range m.areas {
		if a.end > from && a.start < from+n {
			s := a.slice(max(a.start, from), min(a.end, from+n))
			s.start, s.end = s.start-from+to, s.end-from+to
			moved = append(moved, s)
		}
	}
	m.cutAreas(from, from+n)
	m.areas = append(m.areas, moved...)
	sort.Slice(m.areas, func(i, j int) bool { return m.areas[i].start < m.areas[j].start })
	for p := from; p < from+n; p += PageSize {
		pg, ok := m.pages[p]
		if !ok {
			continue
		}
		if err := m.nu.Unmap(m.as, p, PageSize); err != nil {
			return err
		}
		if err := m.nu.Map(m.as, p-from+to, pg.pa, PageSize, pg.hwProt()); err != nil {
			return err
		}
		delete(m.pages, p)
		m.pages[p-from+to] = pg
	}
	return nil
}

var errNoRoom = syscall.ENOMEM

// Resize grows or shrinks the mapping at [old, old+oldN) in place: EFAULT
// if it is not all mapped, errNoRoom if something lies where it would grow.
func (m *Mem) Resize(old, oldN, newN uint64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	switch {
	case !m.covered(old, oldN):
		return syscall.EFAULT
	case newN <= oldN:
		return m.unmapLocked(old+newN, oldN-newN)
	case !m.free(old+oldN, newN-oldN):
		return errNoRoom
	}
	m.extend(old+oldN, newN-oldN)
	return nil
}

// Move shifts the mapping at [from, from+n) to the free range at to and
// grows it to newN bytes there.
func (m *Mem) Move(from, n, to, newN uint64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.covered(from, n) {
		return syscall.EFAULT
	}
	if !m.free(to, newN) {
		return syscall.EINVAL
	}
	if err := m.move(from, to, n); err != nil {
		return err
	}
	if newN > n {
		m.extend(to+n, newN-n)
	}
	return nil
}

// mremap(old, old_size, new_size, flags, new_addr).
func sysMremapFn(p *Process, a [6]uint64) (int64, bool) {
	old, oldN, newN, flags, to := a[0], pageUp(a[1]), pageUp(a[2]), a[3], a[4]
	mayMove, fixed := flags&mremapMaymove != 0, flags&mremapFixed != 0
	switch {
	case old%PageSize != 0 || oldN == 0 || newN == 0 || flags&^(mremapMaymove|mremapFixed) != 0 || fixed && !mayMove:
		return ok(-int64(syscall.EINVAL))
	case fixed && (to%PageSize != 0 || to < old+oldN && old < to+newN):
		return ok(-int64(syscall.EINVAL)) // the target must be aligned and apart from the source
	}
	if !fixed {
		err := p.mem.Resize(old, oldN, newN)
		switch {
		case err == nil:
			return ok(int64(old))
		case err != errNoRoom || !mayMove:
			return ok(errno(err))
		}
		var e int64
		if to, e = p.pickAddr(0, newN, false); e != 0 {
			return ok(e)
		}
	} else {
		p.mem.Unmap(to, newN) // MREMAP_FIXED replaces what was there
	}
	if err := p.mem.Move(old, oldN, to, newN); err != nil {
		return ok(errno(err))
	}
	return ok(int64(to))
}

func init() { table[25] = sysMremapFn }
