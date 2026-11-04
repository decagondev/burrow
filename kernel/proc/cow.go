package proc

// cow.go - copy-on-write fork (W18-S01). Fork shares every private page
// between parent and child instead of copying it: the frame gets a second
// owner in the family's frameRefs, both processes map it read-only, and the
// page is marked cow. Its prot stays what the program asked for; the
// hardware mapping drops the write bit while it is shared. A write - a
// ring-3 fault (FaultIn), the kernel writing user memory (CopyOut, the
// ELF loader) - breaks the share: a page others still own is copied into a
// fresh frame first; the last owner just gets its write bit back. Frames
// are freed only when their last owner lets go. Device pages are never
// counted and never cow. Never copies a page while another owner can write
// it: the copy happens under the table's lock, before the count drops.
// Docs: docs/04-plan/waves/WAVE-18-foundations.md, MEMORY_MAP.md.

import "sync"

// frameRefs counts the owners of shared frames: a frame absent from the
// table has exactly one. One table serves a family of processes (fork
// passes it on), since frames are shared only along fork.
type frameRefs struct {
	mu sync.Mutex
	n  map[uint64]int // pa -> owners, only while there are two or more
}

func newFrameRefs() *frameRefs { return &frameRefs{n: map[uint64]int{}} }

// share adds an owner to pa.
func (r *frameRefs) share(pa uint64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.n[pa] = max(r.n[pa], 1) + 1
}

// release drops an owner; true when that was the last one (free the frame).
func (r *frameRefs) release(pa uint64) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	c := r.n[pa]
	switch {
	case c > 2:
		r.n[pa] = c - 1
	case c == 2:
		delete(r.n, pa)
	default:
		return true
	}
	return false
}

// owners is pa's number of owners (tests).
func (r *frameRefs) owners(pa uint64) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return max(r.n[pa], 1)
}

// hwProt is how a page is mapped: without the write bit while it is cow.
func (pg page) hwProt() Prot {
	if pg.cow {
		return pg.prot &^ ProtWrite
	}
	return pg.prot
}

// releaseLocked gives up pg's frame (m.mu held): freed if this was its
// last owner. Device frames are never freed.
func (m *Mem) releaseLocked(pg page) {
	if pg.shm != nil { // the object's frame: it decides
		pg.shm.unref()
		return
	}
	if !pg.device && m.refs.release(pg.pa) {
		m.nu.PageFree(pg.pa)
	}
}

// remapLocked maps the page at va again with pg's frame and hwProt.
func (m *Mem) remapLocked(va uint64, pg page) error {
	if err := m.nu.Unmap(m.as, va, PageSize); err != nil {
		return err
	}
	return m.nu.Map(m.as, va, pg.pa, PageSize, pg.hwProt())
}

// breakLocked makes the cow page at va this process's own (m.mu held): a
// copy if others own the frame too, the same frame made writable if not.
// The page keeps its prot; only the share ends.
func (m *Mem) breakLocked(va uint64, pg page) (page, error) {
	r := m.refs
	r.mu.Lock()
	if r.n[pg.pa] < 2 { // the last owner: no copy
		r.mu.Unlock()
		pg.cow = false
		m.pages[va] = pg
		return pg, m.remapLocked(va, pg)
	}
	pa, err := m.nu.PageAlloc()
	if err != nil {
		r.mu.Unlock()
		return pg, err
	}
	copy(m.nu.Frame(pa), m.nu.Frame(pg.pa)) // no owner can write it while we hold r.mu
	if r.n[pg.pa]--; r.n[pg.pa] < 2 {
		delete(r.n, pg.pa)
	}
	r.mu.Unlock()
	if err := m.nu.Unmap(m.as, va, PageSize); err != nil {
		return pg, err
	}
	own := page{pa: pa, prot: pg.prot}
	if err := m.nu.Map(m.as, va, pa, PageSize, own.prot); err != nil {
		m.nu.PageFree(pa)
		delete(m.pages, va)
		return pg, err
	}
	m.pages[va] = own
	return own, nil
}

// shareInto gives child c the page at va (fork): device memory as it is,
// every private page as a cow share of the same frame, in both. m.mu held.
func (m *Mem) shareInto(c *Mem, va uint64, pg page) error {
	if pg.device || pg.shm != nil { // shared, as Linux shares MAP_SHARED memory
		if pg.shm != nil {
			pg.shm.ref()
		}
		c.pages[va] = pg
		return c.nu.Map(c.as, va, pg.pa, PageSize, pg.prot)
	}
	if !pg.cow {
		pg.cow = true
		m.pages[va] = pg
		if pg.prot&ProtWrite != 0 {
			if err := m.remapLocked(va, pg); err != nil {
				return err
			}
		}
	}
	m.refs.share(pg.pa)
	c.pages[va] = pg
	return c.nu.Map(c.as, va, pg.pa, PageSize, pg.hwProt())
}
