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
	mu      sync.Mutex
	frames  map[uint64][]byte
	nextPA  uint64
	spaces  map[uint64]map[uint64]mapping // as -> va -> mapping
	nextAS  uint64
	threads map[uint64]*Thread
	nextTID uint64
	reqs    chan proc.Request
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

func (f *Fake) PageAlloc() (uint64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
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
	f.threads[f.nextTID] = &Thread{AS: as, RIP: rip, RSP: rsp, FS: fs, Replies: make(chan int64, 16)}
	return f.nextTID, nil
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
	t.Replies <- result
	return nil
}

// Inject is a user thread making a system call (or taking a fault).
func (f *Fake) Inject(r proc.Request) { f.reqs <- r }

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
