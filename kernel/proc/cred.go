package proc

// cred.go - who a process is (W19-S03): real, effective, saved and
// file-system user and group ids, and supplementary groups, with Linux's
// rules for changing them (setuid(2), setreuid(2), setresuid(2),
// setfsuid(2)). Root is an effective uid of 0: there are no capabilities,
// so root may do what CAP_SETUID and CAP_SETGID allow. Credentials belong
// to the process, not the thread: musl changes every thread's at once, so
// programs cannot tell. Never checks file permissions (W19-S04 does, from
// the fs ids). Docs: docs/04-plan/waves/WAVE-19-unix.md.

import (
	"encoding/binary"
	"slices"
	"syscall"
)

// keep is -1 as an id argument: leave that id as it is.
const keep = ^uint32(0)

// Index into ids.
const (
	idReal = iota
	idEff
	idSaved
	idFS
)

// ids are one kind of id (user or group): real, effective, saved, fs.
type ids [4]uint32

// Cred is a process's identity. The zero value is root.
type Cred struct {
	UID, GID ids
	Groups   []uint32
}

func (c Cred) clone() Cred {
	c.Groups = slices.Clone(c.Groups)
	return c
}

// Root says whether c may do what Linux leaves to CAP_SETUID and friends.
func (c *Cred) Root() bool { return c.UID[idEff] == 0 }

// has says whether id is one of the real, effective and saved ids.
func (x *ids) has(id uint32) bool {
	return id == x[idReal] || id == x[idEff] || id == x[idSaved]
}

// set is setuid(id): root sets all three; others the effective id, to the
// real or the saved one.
func (x *ids) set(id uint32, root bool) error {
	switch {
	case id == keep:
		return syscall.EINVAL
	case root:
		x[idReal], x[idEff], x[idSaved] = id, id, id
	case id == x[idReal] || id == x[idSaved]:
		x[idEff] = id
	default:
		return syscall.EPERM
	}
	x[idFS] = x[idEff]
	return nil
}

// setRE is setreuid(r, e): the real id may become the real or effective
// one, the effective id any of the three; a new real id, or an effective id
// other than the old real one, also becomes the saved id.
func (x *ids) setRE(r, e uint32, root bool) error {
	n := *x
	if r != keep {
		if !root && r != x[idReal] && r != x[idEff] {
			return syscall.EPERM
		}
		n[idReal] = r
	}
	if e != keep {
		if !root && !x.has(e) {
			return syscall.EPERM
		}
		n[idEff] = e
	}
	if r != keep || e != keep && e != x[idReal] {
		n[idSaved] = n[idEff]
	}
	n[idFS] = n[idEff]
	*x = n
	return nil
}

// setRES is setresuid(r, e, s): without root each new id must be one of
// the three already held.
func (x *ids) setRES(r, e, s uint32, root bool) error {
	n := *x
	for i, id := range []uint32{r, e, s} {
		if id == keep {
			continue
		}
		if !root && !x.has(id) {
			return syscall.EPERM
		}
		n[i] = id
	}
	n[idFS] = n[idEff]
	*x = n
	return nil
}

// setFS is setfsuid(id): the old fs id, always; the new one only if root or
// one of the ids held.
func (x *ids) setFS(id uint32, root bool) uint32 {
	old := x[idFS]
	if id != keep && (root || x.has(id) || id == x[idFS]) {
		x[idFS] = id
	}
	return old
}

// Cred is a copy of p's credentials.
func (p *Process) Cred() Cred {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.cred.clone()
}

// peerCred is who sent a socket message or connected (SO_PEERCRED,
// SCM_CREDENTIALS): the pid and, as Linux reports them, the effective ids.
type peerCred struct {
	pid      int32
	uid, gid uint32
}

func (p *Process) peerCred() peerCred {
	p.mu.Lock()
	defer p.mu.Unlock()
	return peerCred{int32(p.PID), p.cred.UID[idEff], p.cred.GID[idEff]}
}

// mayKill is Linux's check for kill(2): root, or the sender's real or
// effective uid is the target's real or saved uid; SIGCONT also within
// a session.
func (p *Process) mayKill(t *Process, sig int) bool {
	from, to := p.Cred(), t.Cred()
	switch {
	case from.Root(), sig == sigCONT && p.SID == t.SID:
		return true
	}
	for _, a := range []uint32{from.UID[idReal], from.UID[idEff]} {
		if a == to.UID[idReal] || a == to.UID[idSaved] {
			return true
		}
	}
	return false
}

// mayClaim checks the struct ucred a sender put in SCM_CREDENTIALS: its own
// pid and ids it holds, as Linux asks; root may claim any.
func (p *Process) mayClaim(b []byte) bool {
	c := p.Cred()
	if c.Root() {
		return true
	}
	le := binary.LittleEndian
	pid, uid, gid := int32(le.Uint32(b)), le.Uint32(b[4:]), le.Uint32(b[8:])
	return int(pid) == p.PID && c.UID.has(uid) && c.GID.has(gid)
}
