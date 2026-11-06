package proc

// semctl.go - semctl(2) (W23-S06, beside sem.go): a set's removal (its
// waiters get EIDRM), its status as struct semid64_ds, and one or all of
// its values read or set. Never waits. Docs: POSIX_COVERAGE.md.

import (
	"encoding/binary"
	"syscall"
	"time"
)

// semctl(id, num, cmd, arg): arg is a value (SETVAL) or an address.
func sysSemctlFn(p *Process, a [6]uint64) (int64, bool) {
	t := &p.sys.sems
	t.mu.Lock()
	defer t.mu.Unlock()
	id, num, cmd := int(a[0]), int(int32(a[1])), a[2]&^0x100 // IPC_64
	s := t.byID[id]
	if s == nil {
		return ok(-int64(syscall.EINVAL))
	}
	one := func() bool { return num >= 0 && num < len(s.vals) }
	switch cmd {
	case ipcRmid:
		delete(t.byID, id)
		s.removed = true
		t.notify()
		return ok(0)
	case ipcStat:
		return ok(errno(p.mem.CopyOut(a[3], s.stat())))
	case ipcSet:
		return ok(0)
	case getVal, getPID, getNcnt, getZcnt:
		if !one() {
			return ok(-int64(syscall.EINVAL))
		}
		return ok(map[uint64]int64{getVal: int64(s.vals[num]), getPID: int64(s.pids[num])}[cmd]) // no counts of waiters: 0
	case setVal:
		v := int(int32(a[3]))
		switch {
		case !one():
			return ok(-int64(syscall.EINVAL))
		case v < 0 || v > semVMX:
			return ok(-int64(syscall.ERANGE))
		}
		s.vals[num], s.pids[num], s.ctime = v, int32(p.PID), time.Now()
		t.notify()
		return ok(0)
	case getAll:
		b := make([]byte, 2*len(s.vals))
		for i, v := range s.vals {
			binary.LittleEndian.PutUint16(b[2*i:], uint16(v))
		}
		return ok(errno(p.mem.CopyOut(a[3], b)))
	case setAll:
		return ok(s.setAll(p, a[3], t))
	}
	return ok(-int64(syscall.EINVAL))
}

// setAll reads every value from va (the table's lock held).
func (s *semSet) setAll(p *Process, va uint64, t *semTab) int64 {
	b := make([]byte, 2*len(s.vals))
	if err := p.mem.CopyIn(va, b); err != nil {
		return errno(err)
	}
	for i := range s.vals {
		if v := int(binary.LittleEndian.Uint16(b[2*i:])); v > semVMX {
			return -int64(syscall.ERANGE)
		}
	}
	for i := range s.vals {
		s.vals[i], s.pids[i] = int(binary.LittleEndian.Uint16(b[2*i:])), int32(p.PID)
	}
	s.ctime = time.Now()
	t.notify()
	return 0
}

// stat is struct semid64_ds: ipc64_perm (key, ids 0, mode), then the last
// semop's and the last change's times, and the number of semaphores.
func (s *semSet) stat() []byte {
	b := make([]byte, semidDsLen)
	le := binary.LittleEndian
	le.PutUint32(b[0:], uint32(s.key))
	le.PutUint16(b[20:], uint16(s.mode))
	if !s.otime.IsZero() {
		le.PutUint64(b[48:], uint64(s.otime.Unix()))
	}
	le.PutUint64(b[64:], uint64(s.ctime.Unix()))
	le.PutUint64(b[80:], uint64(len(s.vals)))
	return b
}
