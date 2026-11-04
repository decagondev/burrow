package proc

// credcalls.go - the id system calls (W19-S03): get*id, getres*id,
// getgroups, set*id, setre*id, setres*id, setfs*id and setgroups, over
// cred.go's rules. The user and group calls are one code path each,
// told apart by which ids they change. Never lets a call change the ids
// halfway: a refusal leaves them as they were. Docs: POSIX_COVERAGE.md.

import (
	"encoding/binary"
	"syscall"
)

const ngroupsMax = 65536 // NGROUPS_MAX

// idsOf is the user (group false) or group ids of c.
func idsOf(c *Cred, group bool) *ids {
	if group {
		return &c.GID
	}
	return &c.UID
}

// getID answers get*id: one of the ids.
func getID(group bool, which int) handler {
	return func(p *Process, a [6]uint64) (int64, bool) {
		c := p.Cred()
		return ok(int64(idsOf(&c, group)[which]))
	}
}

// getRes answers getresuid/getresgid(&r, &e, &s).
func getRes(group bool) handler {
	return func(p *Process, a [6]uint64) (int64, bool) {
		c := p.Cred()
		x := idsOf(&c, group)
		for i := range 3 {
			var b [4]byte
			binary.LittleEndian.PutUint32(b[:], x[i])
			if err := p.mem.CopyOut(a[i], b[:]); err != nil {
				return ok(errno(err))
			}
		}
		return ok(0)
	}
}

// change applies f to p's user or group ids under p.mu, all or nothing.
func (p *Process) change(group bool, f func(x *ids, root bool) error) int64 {
	p.mu.Lock()
	defer p.mu.Unlock()
	n := p.cred.clone()
	if err := f(idsOf(&n, group), p.cred.Root()); err != nil {
		return errno(err)
	}
	p.cred = n
	return 0
}

func setID(group bool) handler {
	return func(p *Process, a [6]uint64) (int64, bool) {
		return ok(p.change(group, func(x *ids, root bool) error { return x.set(uint32(a[0]), root) }))
	}
}

func setRE(group bool) handler {
	return func(p *Process, a [6]uint64) (int64, bool) {
		return ok(p.change(group, func(x *ids, root bool) error { return x.setRE(uint32(a[0]), uint32(a[1]), root) }))
	}
}

func setRES(group bool) handler {
	return func(p *Process, a [6]uint64) (int64, bool) {
		return ok(p.change(group, func(x *ids, root bool) error {
			return x.setRES(uint32(a[0]), uint32(a[1]), uint32(a[2]), root)
		}))
	}
}

func setFS(group bool) handler {
	return func(p *Process, a [6]uint64) (int64, bool) {
		var old uint32
		p.change(group, func(x *ids, root bool) error {
			old = x.setFS(uint32(a[0]), root)
			return nil
		})
		return ok(int64(old))
	}
}

// getgroups(size, list): the count when size is 0, EINVAL when they do not fit.
func sysGetgroupsFn(p *Process, a [6]uint64) (int64, bool) {
	g := p.Cred().Groups
	size := int(int32(a[0]))
	switch {
	case size < 0:
		return ok(-int64(syscall.EINVAL))
	case size == 0:
		return ok(int64(len(g)))
	case size < len(g):
		return ok(-int64(syscall.EINVAL))
	}
	b := make([]byte, 4*len(g))
	for i, id := range g {
		binary.LittleEndian.PutUint32(b[4*i:], id)
	}
	if err := p.mem.CopyOut(a[1], b); err != nil {
		return ok(errno(err))
	}
	return ok(int64(len(g)))
}

// setgroups(size, list): root only.
func sysSetgroupsFn(p *Process, a [6]uint64) (int64, bool) {
	size := int(int32(a[0]))
	if size < 0 || size > ngroupsMax {
		return ok(-int64(syscall.EINVAL))
	}
	b := make([]byte, 4*size)
	if err := p.mem.CopyIn(a[1], b); err != nil {
		return ok(errno(err))
	}
	g := make([]uint32, size)
	for i := range g {
		g[i] = binary.LittleEndian.Uint32(b[4*i:])
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.cred.Root() {
		return ok(-int64(syscall.EPERM))
	}
	p.cred.Groups = g
	return ok(0)
}

func init() {
	for nr, fn := range map[uint64]handler{
		102: getID(false, idReal), 107: getID(false, idEff), 104: getID(true, idReal), 108: getID(true, idEff),
		118: getRes(false), 120: getRes(true), 115: sysGetgroupsFn, 116: sysSetgroupsFn,
		105: setID(false), 106: setID(true), 113: setRE(false), 114: setRE(true),
		117: setRES(false), 119: setRES(true), 122: setFS(false), 123: setFS(true),
	} {
		table[nr] = fn
	}
}
