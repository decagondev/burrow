package proc

import (
	"errors"
	"fmt"
	"sort"
	"sync"
)

// ErrFault is EFAULT: an address the process has no (suitable) page at.
var ErrFault = errors.New("proc: bad user address")

// Mem is one process's memory: an address space in the nucleus, the
// frames proc allocated for it page by page, and the areas whose pages get
// frames when first touched (area.go). Since W18 a frame may be shared
// with other processes of the family (copy-on-write fork, cow.go). Safe
// for concurrent use.
type Mem struct {
	mu    sync.Mutex
	nu    Nucleus
	as    uint64
	pages map[uint64]page // page-aligned va -> its frame
	areas []area          // sorted, disjoint
	refs  *frameRefs      // owners of shared frames, one table per family
}

type page struct {
	pa     uint64
	prot   Prot // what the program may do; the mapping may allow less (cow)
	device bool // device memory (a framebuffer): never freed, shared by fork
	cow    bool // the frame may be shared: mapped read-only until a write
}

func NewMem(nu Nucleus) (*Mem, error) {
	as, err := nu.NewSpace()
	if err != nil {
		return nil, fmt.Errorf("proc: new address space: %w", err)
	}
	return &Mem{nu: nu, as: as, pages: map[uint64]page{}, refs: newFrameRefs()}, nil
}

// Space is the nucleus handle (for StartThread).
func (m *Mem) Space() uint64 { return m.as }

func pageDown(v uint64) uint64 { return v &^ (PageSize - 1) }
func pageUp(v uint64) uint64   { return (v + PageSize - 1) &^ (PageSize - 1) }

// MapAnon backs [va, va+n) with fresh zeroed pages now (ELF segments, the
// first stack). Pages already there keep their contents and get prot added
// (segments may share a page). mmap and brk use Reserve instead.
func (m *Mem) MapAnon(va, n uint64, prot Prot) error {
	if n == 0 || va >= UserTop || n > UserTop-va {
		return ErrFault
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	for p := pageDown(va); p < va+n; p += PageSize {
		if old, ok := m.pages[p]; ok {
			if old.device {
				return ErrFault // device memory is never widened or replaced here
			}
			if old.prot|prot != old.prot { // widen: remap the same frame
				old.prot |= prot
				if err := m.remapLocked(p, old); err != nil {
					return err
				}
				m.pages[p] = old
			}
			continue
		}
		pa, err := m.nu.PageAlloc()
		if err != nil {
			return err
		}
		if err := m.nu.Map(m.as, p, pa, PageSize, prot); err != nil {
			m.nu.PageFree(pa)
			return err
		}
		m.pages[p] = page{pa: pa, prot: prot}
	}
	return nil
}

// MapDevice maps n bytes of device memory at physical pa to va, replacing
// nothing: [va, va+n) must be free. The pages stay the device's.
func (m *Mem) MapDevice(va, pa, n uint64, prot Prot) error {
	if n == 0 || va%PageSize != 0 || pa%PageSize != 0 || va >= UserTop || n > UserTop-va {
		return ErrFault
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	for off := uint64(0); off < n; off += PageSize {
		if _, taken := m.pages[va+off]; taken {
			return ErrFault
		}
	}
	if err := m.nu.Map(m.as, va, pa, n, prot); err != nil {
		return err
	}
	for off := uint64(0); off < n; off += PageSize {
		m.pages[va+off] = page{pa: pa + off, prot: prot, device: true}
	}
	return nil
}

// Unmap removes the areas and frees the pages in [va, va+n) (absent ones
// are skipped).
func (m *Mem) Unmap(va, n uint64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.unmapLocked(va, n)
}

func (m *Mem) unmapLocked(va, n uint64) error {
	m.cutAreas(pageDown(va), pageUp(va+n))
	for p := pageDown(va); p < va+n && p < UserTop; p += PageSize {
		pg, ok := m.pages[p]
		if !ok {
			continue
		}
		if err := m.nu.Unmap(m.as, p, PageSize); err != nil {
			return err
		}
		m.releaseLocked(pg)
		delete(m.pages, p)
	}
	return nil
}

// Mapped reports whether every page of [va, va+n) is backed or reserved.
func (m *Mem) Mapped(va, n uint64) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	for p := pageDown(va); p < va+n; p += PageSize {
		if _, ok := m.pages[p]; !ok {
			if _, inArea := m.areaAt(p); !inArea {
				return false
			}
		}
	}
	return true
}

// access walks [va, va+len(buf)) page by page; each needs `need` in its
// prot. writes: the kernel writes the pages, so a cow page is made this
// process's own first (W18-S01).
func (m *Mem) access(va uint64, n int, need Prot, writes bool, each func(frame []byte, off, done, k int)) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for done := 0; done < n; {
		cur := va + uint64(done)
		pg, ok := m.pages[pageDown(cur)]
		if !ok && cur < UserTop { // first touch of an area page: back it now
			a, inArea := m.areaAt(cur)
			if !inArea || a.none || a.prot&need != need {
				return ErrFault
			}
			var err error
			if pg, err = m.fillLocked(pageDown(cur), a); err != nil {
				return err
			}
			ok = true
		}
		if !ok || pg.prot&need != need || pg.device || cur >= UserTop { // device memory: not through the physmap
			return ErrFault
		}
		if writes && pg.cow {
			var err error
			if pg, err = m.breakLocked(pageDown(cur), pg); err != nil {
				return err
			}
		}
		off := int(cur % PageSize)
		k := min(n-done, PageSize-off)
		each(m.nu.Frame(pg.pa), off, done, k)
		done += k
	}
	return nil
}

// CopyIn reads user memory into dst.
func (m *Mem) CopyIn(va uint64, dst []byte) error {
	return m.access(va, len(dst), 0, false, func(f []byte, off, done, k int) { copy(dst[done:done+k], f[off:off+k]) })
}

// CopyOut writes src to user memory the process may write.
func (m *Mem) CopyOut(va uint64, src []byte) error {
	return m.access(va, len(src), ProtWrite, true, func(f []byte, off, done, k int) { copy(f[off:off+k], src[done:done+k]) })
}

// load writes into any mapped page (the ELF loader filling read-only code).
func (m *Mem) load(va uint64, src []byte) error {
	return m.access(va, len(src), 0, true, func(f []byte, off, done, k int) { copy(f[off:off+k], src[done:done+k]) })
}

// CString reads a NUL-terminated string of at most max bytes.
func (m *Mem) CString(va uint64, max int) (string, error) {
	var out []byte
	b := make([]byte, 1)
	for len(out) < max {
		if err := m.CopyIn(va+uint64(len(out)), b); err != nil {
			return "", err
		}
		if b[0] == 0 {
			return string(out), nil
		}
		out = append(out, b[0])
	}
	return "", ErrFault
}

// Pages lists the mapped pages in address order (for tests and `ps`).
func (m *Mem) Pages() []uint64 {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]uint64, 0, len(m.pages))
	for va := range m.pages {
		out = append(out, va)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

// Clone makes the child's memory for fork: the same areas, and every page
// shared with m copy-on-write (W18-S01; until then an eager copy, W10).
func (m *Mem) Clone() (*Mem, error) {
	c, err := NewMem(m.nu)
	if err != nil {
		return nil, err
	}
	c.refs = m.refs
	m.mu.Lock()
	defer m.mu.Unlock()
	c.areas = append([]area(nil), m.areas...)
	for va, pg := range m.pages {
		if err := m.shareInto(c, va, pg); err != nil {
			c.Close()
			return nil, err
		}
	}
	return c, nil
}

// Close frees every frame and the address space. The process's threads
// must be gone (the nucleus refuses with EBUSY otherwise).
func (m *Mem) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.nu.DestroySpace(m.as); err != nil {
		return err
	}
	for va, pg := range m.pages {
		m.releaseLocked(pg)
		delete(m.pages, va)
	}
	m.areas = nil
	return nil
}
