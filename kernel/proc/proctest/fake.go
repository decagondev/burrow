// Package proctest is a fake nucleus for host tests of package proc: frames
// live in Go memory, address spaces are maps, and a test plays the user
// thread by injecting requests and reading the replies.
package proctest

import (
	"fmt"
	"sync"
	"syscall"

	"burrow/kernel/proc"
)

// Fake implements proc.Nucleus.
type Fake struct {
	mu         sync.Mutex
	frames     map[uint64][]byte
	nextPA     uint64
	limit      int                           // frames PageAlloc may hand out at once; 0 = no limit
	spaces     map[uint64]map[uint64]mapping // as -> va -> mapping
	nextAS     uint64
	threads    map[uint64]*Thread
	nextTID    uint64
	reqs       chan proc.Request
	batchCalls int // Protect and MapBatch calls (W18-S06)
}

type mapping struct {
	pa   uint64
	prot proc.Prot
}

// Thread is what a started user thread looks like to the test.
type Thread struct {
	AS, RIP, RSP, FS uint64
	Killed           bool
	Replies          chan int64 // one value per nu_syscall_reply
	Regs             proc.Regs  // what nu_uthread_regs reads and writes
	Interrupts       int        // nu_uthread_interrupt calls
	busy             bool       // a request is outstanding (Inject .. Reply)
	stopAfter        bool       // interrupted while busy: stop after the reply
	lastNr           uint64     // the outstanding request's number
}

// Snapshot is a copy of a thread's state, safe to read while the
// dispatcher works (the Replies channel is shared, not copied).
func (f *Fake) Snapshot(tid uint64) Thread {
	f.mu.Lock()
	defer f.mu.Unlock()
	t := f.threads[tid]
	if t == nil {
		return Thread{}
	}
	return Thread{AS: t.AS, RIP: t.RIP, RSP: t.RSP, FS: t.FS, Killed: t.Killed,
		Replies: t.Replies, Regs: t.Regs, Interrupts: t.Interrupts}
}

func New() *Fake {
	return &Fake{
		frames: map[uint64][]byte{}, nextPA: 0x100000, spaces: map[uint64]map[uint64]mapping{},
		threads: map[uint64]*Thread{}, nextTID: 100, reqs: make(chan proc.Request, 64),
	}
}

func (f *Fake) NewSpace() (uint64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.nextAS++
	f.spaces[f.nextAS] = map[uint64]mapping{}
	return f.nextAS, nil
}

func (f *Fake) DestroySpace(as uint64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, t := range f.threads {
		if t.AS == as && !t.Killed {
			return syscall.EBUSY
		}
	}
	if _, ok := f.spaces[as]; !ok {
		return syscall.EBADF
	}
	delete(f.spaces, as)
	return nil
}

func (f *Fake) Map(as, va, pa, n uint64, prot proc.Prot) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	s, ok := f.spaces[as]
	if !ok {
		return syscall.EBADF
	}
	if va%proc.PageSize != 0 || n%proc.PageSize != 0 || va+n > proc.UserTop {
		return syscall.EINVAL
	}
	for off := uint64(0); off < n; off += proc.PageSize {
		if _, dup := s[va+off]; dup {
			return syscall.EEXIST
		}
		s[va+off] = mapping{pa + off, prot}
	}
	return nil
}

// Protect implements proc.BatchMapper: mapped pages of the range get prot.
func (f *Fake) Protect(as, va, n uint64, prot proc.Prot) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.batchCalls++
	s, ok := f.spaces[as]
	if !ok {
		return syscall.EBADF
	}
	for off := uint64(0); off < n; off += proc.PageSize {
		if m, mapped := s[va+off]; mapped {
			m.prot = prot
			s[va+off] = m
		}
	}
	return nil
}

// MapBatch implements proc.BatchMapper: Map for each entry.
func (f *Fake) MapBatch(as uint64, e []proc.MapEntry) error {
	f.mu.Lock()
	f.batchCalls++
	f.mu.Unlock()
	if len(e) > proc.MapBatchMax {
		return syscall.EINVAL
	}
	for _, x := range e {
		if err := f.Map(as, x.VA, x.PA, proc.PageSize, proc.Prot(x.Prot)); err != nil {
			return err
		}
	}
	return nil
}

// BatchCalls is how many Protect and MapBatch calls were made (tests).
func (f *Fake) BatchCalls() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.batchCalls
}

func (f *Fake) Unmap(as, va, n uint64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	s := f.spaces[as]
	for off := uint64(0); off < n; off += proc.PageSize {
		if _, ok := s[va+off]; !ok {
			return syscall.EFAULT
		}
		delete(s, va+off)
	}
	return nil
}

// LimitFrames makes PageAlloc fail with ENOMEM once n frames are in use
// (0 removes the limit), as nu_page_alloc does at the kernel's reserve.
func (f *Fake) LimitFrames(n int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.limit = n
}

func (f *Fake) PageAlloc() (uint64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.limit > 0 && len(f.frames) >= f.limit {
		return 0, syscall.ENOMEM
	}
	pa := f.nextPA
	f.nextPA += proc.PageSize
	f.frames[pa] = make([]byte, proc.PageSize)
	return pa, nil
}

func (f *Fake) PageFree(pa uint64) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.frames[pa]; !ok {
		panic(fmt.Sprintf("proctest: double free of frame %#x", pa))
	}
	delete(f.frames, pa)
}

// MemInfo: 1 GiB of RAM (or the LimitFrames limit), less the frames in use.
func (f *Fake) MemInfo() (totalBytes, freeBytes uint64) {
	f.mu.Lock()
	defer f.mu.Unlock()
	total := uint64(1 << 30)
	if f.limit > 0 {
		total = uint64(f.limit) * proc.PageSize
	}
	return total, total - uint64(len(f.frames))*proc.PageSize
}

func (f *Fake) Frame(pa uint64) []byte {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.frames[pa]
}

// FramesInUse counts allocated frames (0 after every process is gone).
func (f *Fake) FramesInUse() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.frames)
}

// PA is the physical address va maps to in as (ok false if it is not mapped).
func (f *Fake) PA(as, va uint64) (uint64, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	m, ok := f.spaces[as][va&^(proc.PageSize-1)]
	return m.pa, ok
}

// Prot is how va is mapped in as (ok false if it is not).
func (f *Fake) Prot(as, va uint64) (proc.Prot, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	m, ok := f.spaces[as][va&^(proc.PageSize-1)]
	return m.prot, ok
}

func (f *Fake) StartThread(as, rip, rsp, fs uint64) (uint64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.nextTID++
	t := &Thread{AS: as, RIP: rip, RSP: rsp, FS: fs, Replies: make(chan int64, 16), Regs: proc.FreshRegs(rip, rsp)}
	t.Regs.FSBase = fs
	f.threads[f.nextTID] = t
	return f.nextTID, nil
}

func (f *Fake) StartThreadFrom(as uint64, r *proc.Regs) (uint64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.spaces[as]; !ok {
		return 0, syscall.EBADF
	}
	f.nextTID++
	f.threads[f.nextTID] = &Thread{AS: as, RIP: r.RIP, RSP: r.RSP, FS: r.FSBase, Replies: make(chan int64, 16), Regs: *r}
	return f.nextTID, nil
}

func (f *Fake) Regs(tid uint64, r *proc.Regs, write bool) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	t, ok := f.threads[tid]
	if !ok || t.Killed {
		return syscall.EBUSY
	}
	if write {
		if r.RIP >= proc.UserTop || r.RSP >= proc.UserTop {
			return syscall.EINVAL
		}
		t.Regs, t.RIP, t.RSP, t.FS = *r, r.RIP, r.RSP, r.FSBase
	} else {
		*r = t.Regs
	}
	return nil
}

func (f *Fake) SetSpace(tid, as uint64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	t, ok := f.threads[tid]
	if !ok {
		return syscall.EBUSY
	}
	if _, ok := f.spaces[as]; !ok {
		return syscall.EBADF
	}
	t.AS = as
	return nil
}

// Interrupt behaves like the nucleus: a thread computing in ring 3 stops at
// once and posts an interrupt request; a thread waiting for an answer
// stops right after that answer.
func (f *Fake) Interrupt(tid uint64) error {
	t := f.Thread(tid)
	if t == nil {
		return syscall.ESRCH
	}
	f.mu.Lock()
	t.Interrupts++
	if t.busy {
		t.stopAfter = true
		f.mu.Unlock()
		return nil
	}
	rip := t.Regs.RIP
	f.mu.Unlock()
	f.Inject(proc.Request{TID: tid, Nr: proc.InterruptNr, Args: [6]uint64{rip}})
	return nil
}

// SetRegs is the test playing the CPU: the thread's registers as they are
// when it next enters the kernel.
func (f *Fake) SetRegs(tid uint64, r proc.Regs) {
	f.mu.Lock()
	defer f.mu.Unlock()
	t := f.threads[tid]
	t.Regs, t.RIP, t.RSP = r, r.RIP, r.RSP
}

// Thread returns a started thread (nil if none).
func (f *Fake) Thread(tid uint64) *Thread {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.threads[tid]
}

func (f *Fake) Kill(tid uint64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	t, ok := f.threads[tid]
	if !ok || t.Killed {
		return syscall.ESRCH
	}
	t.Killed = true
	return nil
}

// Live counts threads that are not killed.
func (f *Fake) Live() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, t := range f.threads {
		if !t.Killed {
			n++
		}
	}
	return n
}

func (f *Fake) SetFS(tid, fs uint64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	t, ok := f.threads[tid]
	if !ok {
		return syscall.ESRCH
	}
	t.FS = fs
	return nil
}

func (f *Fake) Reply(tid uint64, result int64) error {
	t := f.Thread(tid)
	if t == nil {
		return syscall.ESRCH
	}
	f.mu.Lock()
	t.busy = false
	if t.Regs.RAX != uint64(result) {
		t.Regs.RAX = uint64(result) // the answer is RAX (for syscalls)
	}
	stop := t.stopAfter
	t.stopAfter = false
	rip := t.Regs.RIP
	wasStop := t.lastNr == proc.InterruptNr
	f.mu.Unlock()
	if !wasStop { // the answer to an interrupt stop is no reply a test waits for
		t.Replies <- result
	}
	if stop {
		f.Inject(proc.Request{TID: tid, Nr: proc.InterruptNr, Args: [6]uint64{rip}})
	}
	return nil
}

// Inject is a user thread making a system call (or taking a fault).
func (f *Fake) Inject(r proc.Request) {
	if t := f.Thread(r.TID); t != nil {
		f.mu.Lock()
		t.busy, t.lastNr = true, r.Nr
		f.mu.Unlock()
	}
	f.reqs <- r
}

func (f *Fake) Wait(reqs []proc.Request) (int, error) {
	reqs[0] = <-f.reqs
	n := 1
	for n < len(reqs) {
		select {
		case r := <-f.reqs:
			reqs[n] = r
			n++
		default:
			return n, nil
		}
	}
	return n, nil
}
