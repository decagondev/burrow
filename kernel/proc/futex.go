package proc

// futex.go - futex(2) for user programs (W12-S01). A waiter is a channel,
// queued under (address space, address); checking the word and queueing
// happen under one lock, as do waking and requeueing, so no wake-up can
// fall between a waiter's check and its sleep. Futexes are private to an
// address space (MAP_SHARED memory between processes is device memory
// only). Never blocks with futexMu held. Docs: WAVE-12-ports.md.

import (
	"encoding/binary"
	"syscall"
	"time"
)

const (
	futexWait          = 0
	futexWake          = 1
	futexRequeue       = 3
	futexCmpRequeue    = 4
	futexWaitBitset    = 9
	futexWakeBitset    = 10
	futexPrivate       = 128
	futexClockRealtime = 256
	futexCmdMask       = ^uint64(futexPrivate | futexClockRealtime)
)

type futexKey struct {
	mem *Mem
	va  uint64
}

type futexWaiter struct {
	woken  chan struct{} // closed by the waker, who also dequeues it
	bitset uint32
}

// futex(uaddr, op, val, timeout | val2, uaddr2, val3)
func sysFutexFn(t *Thread, a [6]uint64) (int64, bool) {
	p := t.p
	uaddr, op, val := a[0], a[1]&futexCmdMask, uint32(a[2])
	if uaddr%4 != 0 {
		return ok(-int64(syscall.EINVAL))
	}
	switch op {
	case futexWait, futexWaitBitset:
		bitset := ^uint32(0)
		if op == futexWaitBitset {
			bitset = uint32(a[5])
		}
		if bitset == 0 {
			return ok(-int64(syscall.EINVAL))
		}
		timeout, e := futexTimeout(p, a[3], op == futexWaitBitset)
		if e != 0 {
			return ok(e)
		}
		return ok(t.futexWait(uaddr, val, bitset, timeout))
	case futexWake, futexWakeBitset:
		bitset := ^uint32(0)
		if op == futexWakeBitset {
			bitset = uint32(a[5])
		}
		return ok(int64(p.sys.futexWake(p.mem, uaddr, int(val), bitset)))
	case futexRequeue, futexCmpRequeue:
		return ok(p.sys.futexRequeue(p.mem, uaddr, a[4], int(val), int(a[3]), op == futexCmpRequeue, uint32(a[5])))
	}
	return ok(-int64(syscall.ENOSYS))
}

// futexTimeout reads the timespec at va: relative for FUTEX_WAIT, an
// absolute time on the boot clock for FUTEX_WAIT_BITSET. -1: none.
func futexTimeout(p *Process, va uint64, absolute bool) (time.Duration, int64) {
	if va == 0 {
		return -1, 0
	}
	var ts [16]byte
	if err := p.mem.CopyIn(va, ts[:]); err != nil {
		return 0, errno(err)
	}
	sec, nsec := int64(binary.LittleEndian.Uint64(ts[0:])), int64(binary.LittleEndian.Uint64(ts[8:]))
	if sec < 0 || nsec < 0 || nsec >= 1e9 {
		return 0, -int64(syscall.EINVAL)
	}
	d := time.Duration(sec)*time.Second + time.Duration(nsec)
	if absolute {
		d -= time.Since(bootTime) // clock_gettime counts from boot for every clock
	}
	return max(d, 0), 0
}

// futexWait sleeps while the word at uaddr holds val (EAGAIN otherwise)
// until a wake (0), the timeout (ETIMEDOUT) or a signal (EINTR).
func (t *Thread) futexWait(uaddr uint64, val, bitset uint32, timeout time.Duration) int64 {
	p, s := t.p, t.p.sys
	key := futexKey{p.mem, uaddr}
	w := &futexWaiter{woken: make(chan struct{}), bitset: bitset}
	var word [4]byte
	s.futexMu.Lock()
	if err := p.mem.CopyIn(uaddr, word[:]); err != nil {
		s.futexMu.Unlock()
		return errno(err)
	}
	if binary.LittleEndian.Uint32(word[:]) != val {
		s.futexMu.Unlock()
		return -int64(syscall.EAGAIN)
	}
	s.futexes[key] = append(s.futexes[key], w)
	s.futexMu.Unlock()

	var expire <-chan time.Time
	if timeout >= 0 {
		tm := time.NewTimer(timeout)
		defer tm.Stop()
		expire = tm.C
	}
	res := int64(0)
	select {
	case <-w.woken:
		return 0
	case <-expire:
		res = -int64(syscall.ETIMEDOUT)
	case <-t.cancel():
		res = -int64(syscall.EINTR)
	}
	s.futexMu.Lock()
	defer s.futexMu.Unlock()
	select {
	case <-w.woken: // woken while giving up: the wake counts
		return 0
	default:
	}
	s.futexRemove(w)
	return res
}

// futexRemove takes w out of whatever queue holds it (futexMu held).
func (s *System) futexRemove(w *futexWaiter) {
	for k, q := range s.futexes {
		for i, o := range q {
			if o == w {
				s.futexes[k] = append(q[:i:i], q[i+1:]...)
				if len(s.futexes[k]) == 0 {
					delete(s.futexes, k)
				}
				return
			}
		}
	}
}

// futexWake wakes up to n waiters on (mem, uaddr) whose bitset matches.
func (s *System) futexWake(mem *Mem, uaddr uint64, n int, bitset uint32) int {
	s.futexMu.Lock()
	defer s.futexMu.Unlock()
	return s.futexWakeLocked(futexKey{mem, uaddr}, n, bitset)
}

func (s *System) futexWakeLocked(key futexKey, n int, bitset uint32) int {
	woke := 0
	var rest []*futexWaiter
	for _, w := range s.futexes[key] {
		if woke < n && w.bitset&bitset != 0 {
			close(w.woken)
			woke++
		} else {
			rest = append(rest, w)
		}
	}
	if len(rest) == 0 {
		delete(s.futexes, key)
	} else {
		s.futexes[key] = rest
	}
	return woke
}

// futexRequeue wakes n waiters on uaddr and moves up to max of the rest to
// uaddr2; with cmp, only if the word at uaddr still holds val3. All of it
// under one hold of futexMu.
func (s *System) futexRequeue(mem *Mem, uaddr, uaddr2 uint64, n, max int, cmp bool, val3 uint32) int64 {
	s.futexMu.Lock()
	defer s.futexMu.Unlock()
	if cmp {
		var word [4]byte
		if err := mem.CopyIn(uaddr, word[:]); err != nil {
			return errno(err)
		}
		if binary.LittleEndian.Uint32(word[:]) != val3 {
			return -int64(syscall.EAGAIN)
		}
	}
	from, to := futexKey{mem, uaddr}, futexKey{mem, uaddr2}
	woke := s.futexWakeLocked(from, n, ^uint32(0))
	q := s.futexes[from]
	k := min(max, len(q))
	s.futexes[to] = append(s.futexes[to], q[:k]...)
	if k == len(q) {
		delete(s.futexes, from)
	} else {
		s.futexes[from] = q[k:]
	}
	return int64(woke + k)
}

// futexDrop forgets every waiter of an address space that is going away.
func (s *System) futexDrop(mem *Mem) {
	s.futexMu.Lock()
	defer s.futexMu.Unlock()
	for k := range s.futexes {
		if k.mem == mem {
			delete(s.futexes, k)
		}
	}
}

func init() { ttable[202] = sysFutexFn }
