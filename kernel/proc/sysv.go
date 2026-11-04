package proc

// sysv.go - System V shared memory (W18-S04): shmget, shmat, shmdt and
// shmctl over S03's shared objects, for X's MIT-SHM. A segment holds its
// object until IPC_RMID has been called and the last process detached
// (PIT-52); attachments live in each Mem, so fork inherits them and exit
// and execve detach them. Permissions are recorded and reported, never
// enforced (everyone is root). Docs: POSIX_COVERAGE.md.

import (
	"encoding/binary"
	"sync"
	"syscall"
	"time"
)

const (
	ipcPrivate = 0
	ipcCreat   = 0o1000
	ipcExcl    = 0o2000
	ipcRmid    = 0
	ipcSet     = 1
	ipcStat    = 2
	shmRdonly  = 0o10000
	shmRnd     = 0o20000
	shmidDsLen = 112 // struct shmid64_ds on x86-64
	shmMax     = 1 << 32
)

type sysvSeg struct {
	tab     *sysvTab
	id      int
	key     int32
	obj     *shmObj
	size    uint64
	mode    uint32
	cpid    int32
	lpid    int32
	nattch  int
	removed bool
	ctime   time.Time
}

// sysvTab is the system's segments.
type sysvTab struct {
	mu   sync.Mutex
	byID map[int]*sysvSeg
	next int
}

// attach and detach count a process's attachments; the last detach of a
// removed segment frees it. pid is who did it (shm_lpid).
func (g *sysvSeg) attach(pid int32) {
	g.tab.mu.Lock()
	g.nattch++
	g.lpid = pid
	g.tab.mu.Unlock()
}

func (g *sysvSeg) detach(pid int32) {
	g.tab.mu.Lock()
	g.nattch--
	g.lpid = pid
	gone := g.removed && g.nattch == 0
	g.tab.mu.Unlock()
	if gone {
		g.obj.unref()
	}
}

// shmget(key, size, flags).
func sysShmgetFn(p *Process, a [6]uint64) (int64, bool) {
	key, size, flags := int32(a[0]), a[1], a[2]
	t := &p.sys.sysv
	t.mu.Lock()
	defer t.mu.Unlock()
	if key != ipcPrivate {
		for _, g := range t.byID {
			if g.key != key || g.removed {
				continue
			}
			switch {
			case flags&ipcCreat != 0 && flags&ipcExcl != 0:
				return ok(-int64(syscall.EEXIST))
			case size > g.size:
				return ok(-int64(syscall.EINVAL))
			}
			return ok(int64(g.id))
		}
		if flags&ipcCreat == 0 {
			return ok(-int64(syscall.ENOENT))
		}
	}
	if size == 0 || size > shmMax {
		return ok(-int64(syscall.EINVAL))
	}
	if t.byID == nil {
		t.byID = map[int]*sysvSeg{}
	}
	t.next++
	o := newShmObj(p.sys.nu, "SYSV", int64(size))
	o.ref() // the segment's own hold, until IPC_RMID and the last detach
	g := &sysvSeg{tab: t, id: t.next, key: key, obj: o, size: size, mode: uint32(flags & 0o777),
		cpid: int32(p.PID), ctime: time.Now()}
	t.byID[g.id] = g
	return ok(int64(g.id))
}

func (t *sysvTab) get(id uint64) *sysvSeg {
	t.mu.Lock()
	defer t.mu.Unlock()
	if g := t.byID[int(id)]; g != nil && !(g.removed && g.nattch == 0) {
		return g
	}
	return nil
}

// shmat(id, addr, flags): maps the segment shared at addr or where there
// is room; returns where.
func sysShmatFn(p *Process, a [6]uint64) (int64, bool) {
	g := p.sys.sysv.get(a[0])
	if g == nil {
		return ok(-int64(syscall.EINVAL))
	}
	addr, flags := a[1], a[2]
	if flags&shmRnd != 0 {
		addr = pageDown(addr)
	}
	pr := ProtWrite
	if flags&shmRdonly != 0 {
		pr = 0
	}
	at := p.mmapShared(g.obj, 0, pageUp(g.size), pr, false, addr != 0, addr)
	if at < 0 {
		return ok(at)
	}
	p.mem.attachSysv(uint64(at), g)
	g.attach(int32(p.PID))
	return ok(at)
}

// shmdt(addr): unmaps the segment attached there.
func sysShmdtFn(p *Process, a [6]uint64) (int64, bool) {
	g := p.mem.detachSysv(a[0])
	if g == nil {
		return ok(-int64(syscall.EINVAL))
	}
	if err := p.mem.Unmap(a[0], pageUp(g.size)); err != nil {
		return ok(errno(err))
	}
	g.detach(int32(p.PID))
	return ok(0)
}

// shmctl(id, cmd, buf): IPC_RMID, IPC_STAT, IPC_SET (accepted).
func sysShmctlFn(p *Process, a [6]uint64) (int64, bool) {
	g := p.sys.sysv.get(a[0])
	if g == nil {
		return ok(-int64(syscall.EINVAL))
	}
	switch a[1] &^ 0x100 { // IPC_64
	case ipcRmid:
		g.tab.mu.Lock()
		first := !g.removed
		g.removed = true
		gone := first && g.nattch == 0
		g.tab.mu.Unlock()
		if gone {
			g.obj.unref()
		}
		return ok(0)
	case ipcStat:
		return ok(errno(p.mem.CopyOut(a[2], g.stat())))
	case ipcSet:
		return ok(0)
	}
	return ok(-int64(syscall.EINVAL))
}

// stat is struct shmid64_ds: ipc64_perm (key, uid, gid, cuid, cgid, mode,
// seq), then size, the times, cpid, lpid and nattch.
func (g *sysvSeg) stat() []byte {
	g.tab.mu.Lock()
	defer g.tab.mu.Unlock()
	b := make([]byte, shmidDsLen)
	le := binary.LittleEndian
	le.PutUint32(b[0:], uint32(g.key))
	le.PutUint16(b[20:], uint16(g.mode))
	le.PutUint64(b[48:], g.size)
	le.PutUint64(b[72:], uint64(g.ctime.Unix()))
	le.PutUint32(b[80:], uint32(g.cpid))
	le.PutUint32(b[84:], uint32(g.lpid))
	le.PutUint64(b[88:], uint64(g.nattch))
	return b
}

// attachSysv and detachSysv keep a Mem's attachments (fork copies them in
// Clone, Close detaches them).
func (m *Mem) attachSysv(at uint64, g *sysvSeg) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.sysv == nil {
		m.sysv = map[uint64]*sysvSeg{}
	}
	m.sysv[at] = g
}

func (m *Mem) detachSysv(at uint64) *sysvSeg {
	m.mu.Lock()
	defer m.mu.Unlock()
	g := m.sysv[at]
	delete(m.sysv, at)
	return g
}

func init() {
	table[29] = sysShmgetFn
	table[30] = sysShmatFn
	table[31] = sysShmctlFn
	table[67] = sysShmdtFn
}
