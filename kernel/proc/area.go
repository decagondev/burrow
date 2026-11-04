package proc

// area.go - demand-zero user memory (W12-S03; the user-space twin of
// BUG-0012). mmap and brk record an area and allocate nothing; a page gets
// its frame the first time it is touched: a ring-3 page fault inside the
// area (answered, the thread resumes) or the kernel copying in or out of
// it. PROT_NONE areas get no frames: touching them is SIGSEGV. The Go
// runtime reserves hundreds of MiB it never touches; this is why that
// works. Areas never overlap and are kept sorted; m.mu guards them.
//
// An area may have a backing file (W14-S05, mmap of files): its pages
// fill from the file instead of with zeros, and what lies past the end of
// the file reads as zeros. The pages are private copies; nothing is ever
// written back. An area keeps its io.ReaderAt after the descriptor is
// closed, which is safe because no Burrow file system's Close does
// anything (memfs, tarfs, FAT, wfile).
// Docs: docs/02-architecture/MEMORY_MAP.md (user memory).

import (
	"errors"
	"io"
	"sort"
)

// area is [start, end) of lazily backed memory.
type area struct {
	start, end uint64
	prot       Prot
	none       bool        // PROT_NONE: reserved, not accessible
	file       io.ReaderAt // nil: anonymous (zeros)
	off        int64       // the file offset at start
	program    bool        // a program's segment (execve): faults read around (W18-S02)
	shm        *shmObj     // MAP_SHARED of a shared object: its frames, from off (W18-S03)
}

// slice is the part [lo, hi) of a (inside it), its file offset moved along.
func (a area) slice(lo, hi uint64) area {
	a.off += int64(lo - a.start)
	a.start, a.end = lo, hi
	return a
}

// Reserve makes [va, va+n) an area with prot (none: PROT_NONE), replacing
// any area there; pages already backed in the range stay as they are.
func (m *Mem) Reserve(va, n uint64, prot Prot, none bool) error {
	return m.reserve(area{start: va, end: va + n, prot: prot, none: none})
}

// ReserveFile is Reserve for a private mapping of file from offset off.
func (m *Mem) ReserveFile(va, n uint64, prot Prot, none bool, file io.ReaderAt, off int64) error {
	return m.reserve(area{start: va, end: va + n, prot: prot, none: none, file: file, off: off})
}

// ReserveShared is Reserve for a MAP_SHARED mapping of o from offset off:
// its pages are o's frames, the same in every process that maps them.
func (m *Mem) ReserveShared(va, n uint64, prot Prot, none bool, o *shmObj, off int64) error {
	return m.reserve(area{start: va, end: va + n, prot: prot, none: none, shm: o, off: off})
}

// reserveProgram is ReserveFile for a program's segment (execve, elf.go):
// its faults read the window around them, since a program touches most of
// its pages; ordinary mappings do not (fontconfig reads a little of many
// fonts, and reading around those cost st a quarter second).
func (m *Mem) reserveProgram(va, n uint64, prot Prot, file io.ReaderAt, off int64) error {
	return m.reserve(area{start: va, end: va + n, prot: prot, file: file, off: off, program: true})
}

func (m *Mem) reserve(a area) error {
	va, n := a.start, a.end-a.start
	if n == 0 || va%PageSize != 0 || va >= UserTop || n > UserTop-va {
		return ErrFault
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	a.end = va + pageUp(n)
	m.cutAreas(va, a.end)
	m.areas = append(m.areas, a)
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
			out = append(out, a.slice(a.start, lo))
		}
		if a.end > hi {
			out = append(out, a.slice(hi, a.end))
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
			part := a.slice(max(a.start, lo), min(a.end, hi))
			part.prot, part.none = prot, none
			inside = append(inside, part)
		}
	}
	m.cutAreas(lo, hi)
	m.areas = append(m.areas, inside...)
	sort.Slice(m.areas, func(i, j int) bool { return m.areas[i].start < m.areas[j].start })
}

// fillLocked backs page p of an accessible area with a frame: zeroed, or
// read from the area's file (m.mu held).
func (m *Mem) fillLocked(p uint64, a area) (page, error) {
	if a.program {
		return m.fillAroundLocked(p, a)
	}
	if a.shm != nil {
		pa, err := a.shm.frame((a.off + int64(p-a.start)) / PageSize)
		if err != nil {
			return page{}, err
		}
		if err := m.nu.Map(m.as, p, pa, PageSize, a.prot); err != nil {
			return page{}, err
		}
		a.shm.ref()
		pg := page{pa: pa, prot: a.prot, shm: a.shm}
		m.pages[p] = pg
		return pg, nil
	}
	pa, err := m.nu.PageAlloc()
	if err != nil {
		return page{}, err
	}
	if a.file != nil {
		_, err := a.file.ReadAt(m.nu.Frame(pa), a.off+int64(p-a.start)) // short at the end: the rest stays zero
		if err != nil && err != io.EOF {
			m.nu.PageFree(pa)
			return page{}, &fileError{err}
		}
	}
	return m.mapFilledLocked(p, pa, a)
}

// faultAround is how much of a program's file a fault reads at once
// (W18-S02, as Linux's fault_around_bytes): one read of the aligned window
// around the page, so a program that touches most of its pages reads its
// file in runs, as execve did before it was lazy, not 4 KiB at a time.
const faultAround = 64 << 10

// fillAroundLocked fills p from a's file and maps, from the same read, the
// other pages of p's window that are in a and not backed yet. Only p must
// succeed; a neighbour without a frame is left for its own fault.
func (m *Mem) fillAroundLocked(p uint64, a area) (page, error) {
	lo := max(a.start, p&^(faultAround-1))
	hi := min(a.end, lo+faultAround)
	buf := make([]byte, hi-lo)
	if _, err := a.file.ReadAt(buf, a.off+int64(lo-a.start)); err != nil && err != io.EOF {
		return page{}, &fileError{err} // short at the end of the file: the rest stays zero
	}
	var got page
	for q := lo; q < hi; q += PageSize {
		if _, backed := m.pages[q]; backed && q != p {
			continue
		}
		pa, err := m.nu.PageAlloc()
		if err != nil {
			if q == p {
				return page{}, err
			}
			continue
		}
		copy(m.nu.Frame(pa), buf[q-lo:q-lo+PageSize])
		pg, err := m.mapFilledLocked(q, pa, a)
		if q == p {
			if err != nil {
				return page{}, err
			}
			got = pg
		}
	}
	return got, nil
}

// mapFilledLocked maps the filled frame pa at p with a's protection.
func (m *Mem) mapFilledLocked(p, pa uint64, a area) (page, error) {
	if err := m.nu.Map(m.as, p, pa, PageSize, a.prot); err != nil {
		m.nu.PageFree(pa)
		return page{}, err
	}
	pg := page{pa: pa, prot: a.prot}
	m.pages[p] = pg
	return pg, nil
}

// Fault outcomes for FaultIn.
type Fault int

const (
	FaultResume Fault = iota // the page now has a frame: the thread may resume
	FaultSignal              // a real fault: SIGSEGV
	FaultNoMem               // the page is allowed but no frame is left: kill (W14-S01)
)

// FaultIn answers a ring-3 page fault at va: FaultResume if va is in an
// area that allows the access and its page now has a frame, FaultNoMem if
// it would but the nucleus has no frame to give, FaultSignal otherwise.
func (m *Mem) FaultIn(va uint64, write bool) Fault {
	m.mu.Lock()
	defer m.mu.Unlock()
	p := pageDown(va)
	if va >= UserTop {
		return FaultSignal
	}
	if pg, backed := m.pages[p]; backed {
		if !write || !pg.cow || pg.prot&ProtWrite == 0 {
			return FaultSignal // the page is there: a protection fault
		}
		if _, err := m.breakLocked(p, pg); err != nil { // a write to a shared page (W18-S01)
			return FaultNoMem
		}
		return FaultResume
	}
	a, ok := m.areaAt(va)
	if !ok || a.none || write && a.prot&ProtWrite == 0 {
		return FaultSignal
	}
	if _, err := m.fillLocked(p, a); err != nil {
		if _, isFile := err.(*fileError); isFile {
			return FaultSignal // the file could not be read (Linux: SIGBUS)
		}
		return FaultNoMem
	}
	return FaultResume
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
		pg.prot = prot // a cow page stays mapped read-only (hwProt)
		if err := m.remapLocked(p, pg); err != nil {
			return err
		}
		m.pages[p] = pg
	}
	return nil
}

var errNotMapped = errors.New("proc: range not mapped")

// fileError is a mapped file's read failing while a page fills.
type fileError struct{ err error }

func (e *fileError) Error() string { return "proc: mapped file: " + e.err.Error() }

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
			m.releaseLocked(pg)
			delete(m.pages, p)
			continue
		}
		if pg.cow { // clearing a shared frame would clear it for its other owners
			var err error
			if pg, err = m.breakLocked(p, pg); err != nil {
				continue
			}
		}
		clear(m.nu.Frame(pg.pa))
	}
}
