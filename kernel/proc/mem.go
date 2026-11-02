package proc

import (
	"errors"
	"fmt"
	"sort"
	"sync"
)

// ErrFault is EFAULT: an address the process has no (suitable) page at.
var ErrFault = errors.New("proc: bad user address")

// Mem is one process's memory: an address space in the nucleus plus the
// frames proc allocated for it, page by page. Safe for concurrent use.
type Mem struct {
	mu    sync.Mutex
	nu    Nucleus
	as    uint64
	pages map[uint64]page // page-aligned va -> its frame
}

type page struct {
	pa   uint64
	prot Prot
}

func NewMem(nu Nucleus) (*Mem, error) {
	as, err := nu.NewSpace()
	if err != nil {
		return nil, fmt.Errorf("proc: new address space: %w", err)
	}
	return &Mem{nu: nu, as: as, pages: map[uint64]page{}}, nil
}

// Space is the nucleus handle (for StartThread).
func (m *Mem) Space() uint64 { return m.as }

func pageDown(v uint64) uint64 { return v &^ (PageSize - 1) }
func pageUp(v uint64) uint64   { return (v + PageSize - 1) &^ (PageSize - 1) }

// MapAnon backs [va, va+n) with fresh zeroed pages. Pages already there
// keep their contents and get prot added (segments may share a page).
func (m *Mem) MapAnon(va, n uint64, prot Prot) error {
	if n == 0 || va >= UserTop || n > UserTop-va {
		return ErrFault
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	for p := pageDown(va); p < va+n; p += PageSize {
		if old, ok := m.pages[p]; ok {
			if old.prot|prot != old.prot { // widen: remap the same frame
				if err := m.nu.Unmap(m.as, p, PageSize); err != nil {
					return err
				}
				old.prot |= prot
				if err := m.nu.Map(m.as, p, old.pa, PageSize, old.prot); err != nil {
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
		m.pages[p] = page{pa, prot}
	}
	return nil
}

// Unmap removes and frees the pages in [va, va+n) (absent ones are skipped).
func (m *Mem) Unmap(va, n uint64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for p := pageDown(va); p < va+n && p < UserTop; p += PageSize {
		pg, ok := m.pages[p]
		if !ok {
			continue
		}
		if err := m.nu.Unmap(m.as, p, PageSize); err != nil {
			return err
		}
		m.nu.PageFree(pg.pa)
		delete(m.pages, p)
	}
	return nil
}

// Mapped reports whether every page of [va, va+n) is present.
func (m *Mem) Mapped(va, n uint64) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	for p := pageDown(va); p < va+n; p += PageSize {
		if _, ok := m.pages[p]; !ok {
			return false
		}
	}
	return true
}

// access walks [va, va+len(buf)) page by page; each needs `need` in its prot.
func (m *Mem) access(va uint64, n int, need Prot, each func(frame []byte, off, done, k int)) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for done := 0; done < n; {
		cur := va + uint64(done)
		pg, ok := m.pages[pageDown(cur)]
		if !ok || pg.prot&need != need || cur >= UserTop {
			return ErrFault
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
	return m.access(va, len(dst), 0, func(f []byte, off, done, k int) { copy(dst[done:done+k], f[off:off+k]) })
}

// CopyOut writes src to user memory the process may write.
func (m *Mem) CopyOut(va uint64, src []byte) error {
	return m.access(va, len(src), ProtWrite, func(f []byte, off, done, k int) { copy(f[off:off+k], src[done:done+k]) })
}

// load writes into any mapped page (the ELF loader filling read-only code).
func (m *Mem) load(va uint64, src []byte) error {
	return m.access(va, len(src), 0, func(f []byte, off, done, k int) { copy(f[off:off+k], src[done:done+k]) })
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

// Clone copies m page by page into a new address space (fork, W10: an
// eager copy; copy-on-write would share frames until a write fault).
func (m *Mem) Clone() (*Mem, error) {
	c, err := NewMem(m.nu)
	if err != nil {
		return nil, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	for va, pg := range m.pages {
		pa, err := c.nu.PageAlloc()
		if err != nil {
			c.Close()
			return nil, err
		}
		copy(c.nu.Frame(pa), m.nu.Frame(pg.pa))
		if err := c.nu.Map(c.as, va, pa, PageSize, pg.prot); err != nil {
			c.nu.PageFree(pa)
			c.Close()
			return nil, err
		}
		c.pages[va] = page{pa, pg.prot}
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
		m.nu.PageFree(pg.pa)
		delete(m.pages, va)
	}
	return nil
}
