package proc

// area.go - demand-zero user memory (W12-S03; the user-space twin of
// BUG-0012). mmap and brk record an area and allocate nothing; a page gets
// its frame the first time it is touched: a ring-3 page fault inside the
// area (answered, the thread resumes) or the kernel copying in or out of
// it. PROT_NONE areas get no frames: touching them is SIGSEGV. The Go
// runtime reserves hundreds of MiB it never touches; this is why that
// works. Areas never overlap and are kept sorted; m.mu guards them.
// Docs: docs/02-architecture/MEMORY_MAP.md (user memory).

import (
	"errors"
	"sort"
)

// area is [start, end) of lazily backed anonymous memory.
type area struct {
	start, end uint64
	prot       Prot
	none       bool // PROT_NONE: reserved, not accessible
}

// Reserve makes [va, va+n) an area with prot (none: PROT_NONE), replacing
// any area there; pages already backed in the range stay as they are.
func (m *Mem) Reserve(va, n uint64, prot Prot, none bool) error {
	if n == 0 || va%PageSize != 0 || va >= UserTop || n > UserTop-va {
		return ErrFault
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	end := va + pageUp(n)
	m.cutAreas(va, end)
	m.areas = append(m.areas, area{va, end, prot, none})
	sort.Slice(m.areas, func(i, j int) bool { return m.areas[i].start < m.areas[j].start })
	return nil
}

// cutAreas removes [lo, hi) from every area, splitting where needed (m.mu held).
func (m *Mem) cutAreas(lo, hi uint64) {
	var out []area
	for _, a := range m.areas {
		if a.end <= lo || a.start >= hi {
			out = append(out, a)
			continue
		}
		if a.start < lo {
			out = append(out, area{a.start, lo, a.prot, a.none})
		}
		if a.end > hi {
			out = append(out, area{hi, a.end, a.prot, a.none})
		}
	}
	m.areas = out
}

// areaAt is the area holding va (m.mu held).
func (m *Mem) areaAt(va uint64) (area, bool) {
	i := sort.Search(len(m.areas), func(i int) bool { return m.areas[i].end > va })
	if i < len(m.areas) && m.areas[i].start <= va {
		return m.areas[i], true
	}
	return area{}, false
}

// reprotect sets prot on the areas inside [lo, hi), splitting at the edges
// (m.mu held).
func (m *Mem) reprotect(lo, hi uint64, prot Prot, none bool) {
	var inside []area
	for _, a := range m.areas {
		if a.end > lo && a.start < hi {
			inside = append(inside, area{max(a.start, lo), min(a.end, hi), prot, none})
		}
	}
	m.cutAreas(lo, hi)
	m.areas = append(m.areas, inside...)
	sort.Slice(m.areas, func(i, j int) bool { return m.areas[i].start < m.areas[j].start })
}

// fillLocked backs page p of an accessible area with a zeroed frame (m.mu held).
func (m *Mem) fillLocked(p uint64, a area) (page, error) {
	pa, err := m.nu.PageAlloc()
	if err != nil {
		return page{}, err
	}
	if err := m.nu.Map(m.as, p, pa, PageSize, a.prot); err != nil {
		m.nu.PageFree(pa)
		return page{}, err
	}
	pg := page{pa: pa, prot: a.prot}
	m.pages[p] = pg
	return pg, nil
}

// FaultIn answers a ring-3 page fault at va: true if va is in an area that
// allows the access and its page now has a frame (the thread may resume);
// false for a real fault (a SIGSEGV).
func (m *Mem) FaultIn(va uint64, write bool) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	p := pageDown(va)
	if _, backed := m.pages[p]; backed || va >= UserTop {
		return false // the page is there: a protection fault
	}
	a, ok := m.areaAt(va)
	if !ok || a.none || write && a.prot&ProtWrite == 0 {
		return false
	}
	_, err := m.fillLocked(p, a)
	return err == nil
}

// Areas lists the areas (tests).
func (m *Mem) Areas() [][2]uint64 {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([][2]uint64, len(m.areas))
	for i, a := range m.areas {
		out[i] = [2]uint64{a.start, a.end}
	}
	return out
}

// Protect changes the protection of [va, va+n), every page of which must
// be backed or in an area (ENOMEM otherwise, as mprotect). none makes the
// areas PROT_NONE; pages already backed become read-only (the nucleus has
// no "mapped but inaccessible").
func (m *Mem) Protect(va, n uint64, prot Prot, none bool) error {
	if va%PageSize != 0 || va >= UserTop || n > UserTop-va {
		return ErrFault
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	for p := va; p < va+n; p += PageSize {
		if _, ok := m.pages[p]; !ok {
			if _, inArea := m.areaAt(p); !inArea {
				return errNotMapped
			}
		}
	}
	m.reprotect(va, va+n, prot, none)
	for p := va; p < va+n; p += PageSize {
		pg, ok := m.pages[p]
		if !ok || pg.prot == prot {
			continue
		}
		if err := m.nu.Unmap(m.as, p, PageSize); err != nil {
			return err
		}
		if err := m.nu.Map(m.as, p, pg.pa, PageSize, prot); err != nil {
			return err
		}
		pg.prot = prot
		m.pages[p] = pg
	}
	return nil
}

var errNotMapped = errors.New("proc: range not mapped")

// Zero makes [va, va+n) read as zero (madvise MADV_DONTNEED, BUG-0009):
// pages in an area give their frames back (they fill again on touch),
// others are cleared.
func (m *Mem) Zero(va, n uint64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for p := pageDown(va); p < va+n && p < UserTop; p += PageSize {
		pg, ok := m.pages[p]
		if !ok || pg.device {
			continue
		}
		if _, inArea := m.areaAt(p); inArea && m.nu.Unmap(m.as, p, PageSize) == nil {
			m.nu.PageFree(pg.pa)
			delete(m.pages, p)
			continue
		}
		clear(m.nu.Frame(pg.pa))
	}
}
