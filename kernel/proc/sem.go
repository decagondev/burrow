package proc

// sem.go - System V semaphores (W23-S06; busybox's syslogd and logread lock
// their shared log buffer with them): semget, semop and semtimedop (every
// operation of a call applies at once or none does; a call that cannot
// waits, or EAGAIN with IPC_NOWAIT), semctl (IPC_RMID, IPC_STAT, IPC_SET,
// GETVAL, SETVAL, GETALL, SETALL, GETPID, GETNCNT, GETZCNT). SEM_UNDO's
// adjustments are kept per process and applied at its exit; fork does not
// inherit them, as on Linux. Permissions are recorded, not enforced, as
// for the segments (sysv.go). Never holds the table's lock while waiting.
// Docs: docs/02-architecture/POSIX_COVERAGE.md.

import (
	"encoding/binary"
	"sync"
	"syscall"
	"time"
)

const (
	semUndo    = 0x1000 // SEM_UNDO
	ipcNowait  = 0o4000 // IPC_NOWAIT
	semMSL     = 32000  // SEMMSL: semaphores in a set
	semOPM     = 500    // SEMOPM: operations in a call
	semVMX     = 32767  // SEMVMX: the largest value
	semidDsLen = 104    // struct semid64_ds on x86-64
	sembufLen  = 6      // struct sembuf: num u16, op s16, flg s16

	getPID, getVal, getAll, getNcnt, getZcnt = 11, 12, 13, 14, 15
	setVal, setAll                           = 16, 17
)

type semSet struct {
	id           int
	key          int32
	vals         []int
	pids         []int32
	mode         uint32
	removed      bool
	otime, ctime time.Time
}

// semTab is the system's semaphore sets; changed wakes every waiter.
type semTab struct {
	mu      sync.Mutex
	byID    map[int]*semSet
	next    int
	changed chan struct{}
}

func (t *semTab) notify() {
	if t.changed != nil {
		close(t.changed)
	}
	t.changed = make(chan struct{})
}

// semKey names one semaphore for SEM_UNDO's adjustments.
type semKey struct{ id, num int }

// semget(key, nsems, flags).
func sysSemgetFn(p *Process, a [6]uint64) (int64, bool) {
	key, nsems, flags := int32(a[0]), int(int32(a[1])), a[2]
	t := &p.sys.sems
	t.mu.Lock()
	defer t.mu.Unlock()
	if key != ipcPrivate {
		for _, s := range t.byID {
			if s.key != key {
				continue
			}
			switch {
			case flags&ipcCreat != 0 && flags&ipcExcl != 0:
				return ok(-int64(syscall.EEXIST))
			case nsems > len(s.vals):
				return ok(-int64(syscall.EINVAL))
			}
			return ok(int64(s.id))
		}
		if flags&ipcCreat == 0 {
			return ok(-int64(syscall.ENOENT))
		}
	}
	if nsems <= 0 || nsems > semMSL {
		return ok(-int64(syscall.EINVAL))
	}
	if t.byID == nil {
		t.byID = map[int]*semSet{}
		t.notify()
	}
	t.next++
	s := &semSet{id: t.next, key: key, vals: make([]int, nsems), pids: make([]int32, nsems),
		mode: uint32(flags & 0o777), ctime: time.Now()}
	t.byID[s.id] = s
	return ok(int64(s.id))
}

type sembuf struct {
	num     int
	op, flg int16
}

// semop(id, sops, nsops) and semtimedop(id, sops, nsops, timeout).
func sysSemopFn(t *Thread, a [6]uint64) (int64, bool) { return ok(t.semop(a[0], a[1], a[2], 0)) }

func sysSemtimedopFn(t *Thread, a [6]uint64) (int64, bool) {
	return ok(t.semop(a[0], a[1], a[2], a[3]))
}

func (t *Thread) semop(id, sopsVA, nsops, timeoutVA uint64) int64 {
	p := t.p
	if nsops == 0 || nsops > semOPM {
		return -int64(syscall.E2BIG)
	}
	raw := make([]byte, sembufLen*nsops)
	if err := p.mem.CopyIn(sopsVA, raw); err != nil {
		return errno(err)
	}
	ops := make([]sembuf, nsops)
	for i := range ops {
		b := raw[sembufLen*i:]
		ops[i] = sembuf{int(binary.LittleEndian.Uint16(b)), int16(binary.LittleEndian.Uint16(b[2:])), int16(binary.LittleEndian.Uint16(b[4:]))}
	}
	var deadline <-chan time.Time
	if timeoutVA != 0 {
		var ts [16]byte
		if err := p.mem.CopyIn(timeoutVA, ts[:]); err != nil {
			return errno(err)
		}
		d := time.Duration(binary.LittleEndian.Uint64(ts[:]))*time.Second + time.Duration(binary.LittleEndian.Uint64(ts[8:]))
		deadline = time.After(d)
	}
	for {
		res, wait := p.semTry(int(id), ops)
		if wait == nil {
			return res
		}
		select {
		case <-wait:
		case <-deadline:
			return -int64(syscall.EAGAIN)
		case <-t.cancel():
			return -int64(syscall.EINTR)
		}
	}
}

// semTry applies ops if all can go now (0), fails (-errno), or says what to
// wait for (a channel closed at the next change).
func (p *Process) semTry(id int, ops []sembuf) (int64, <-chan struct{}) {
	t := &p.sys.sems
	t.mu.Lock()
	defer t.mu.Unlock()
	s := t.byID[id]
	if s == nil {
		return -int64(syscall.EIDRM), nil
	}
	next := append([]int(nil), s.vals...)
	for _, o := range ops {
		switch {
		case o.num >= len(next):
			return -int64(syscall.EFBIG), nil
		case o.op > 0 && next[o.num]+int(o.op) > semVMX:
			return -int64(syscall.ERANGE), nil
		case o.op < 0 && next[o.num] < -int(o.op), o.op == 0 && next[o.num] != 0:
			if o.flg&ipcNowait != 0 {
				return -int64(syscall.EAGAIN), nil
			}
			return 0, t.changed
		}
		next[o.num] += int(o.op)
	}
	s.vals, s.otime = next, time.Now()
	p.mu.Lock()
	for _, o := range ops {
		s.pids[o.num] = int32(p.PID)
		if o.flg&semUndo != 0 && o.op != 0 {
			if p.semAdj == nil {
				p.semAdj = map[semKey]int{}
			}
			p.semAdj[semKey{id, o.num}] -= int(o.op)
		}
	}
	p.mu.Unlock()
	t.notify()
	return 0, nil
}

// semExit applies p's SEM_UNDO adjustments (its exit).
func (p *Process) semExit() {
	p.mu.Lock()
	adj := p.semAdj
	p.semAdj = nil
	p.mu.Unlock()
	if len(adj) == 0 {
		return
	}
	t := &p.sys.sems
	t.mu.Lock()
	defer t.mu.Unlock()
	for k, d := range adj {
		if s := t.byID[k.id]; s != nil && k.num < len(s.vals) {
			s.vals[k.num] = min(max(s.vals[k.num]+d, 0), semVMX)
		}
	}
	t.notify()
}

func init() {
	table[64] = sysSemgetFn
	table[66] = sysSemctlFn
	ttable[65] = sysSemopFn
	ttable[220] = sysSemtimedopFn
}
