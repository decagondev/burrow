package proc

// limits.go - madvise, prlimit64, getrlimit and setrlimit (W12-S02): what
// the Go runtime and libc ask at start-up. Limits are fixed and reported
// honestly; raising a soft limit up to the hard one is accepted. Docs:
// docs/02-architecture/POSIX_COVERAGE.md.

import (
	"encoding/binary"
	"syscall"
)

const (
	madvDontneed = 4
	rlimInfinity = ^uint64(0)
	rlimitStack  = 3
	rlimitNofile = 7
	rlimitNlimit = 16
)

// madvise: MADV_DONTNEED zero-fills; every other advice is accepted.
func sysMadviseFn(p *Process, a [6]uint64) (int64, bool) {
	if a[0]%PageSize != 0 {
		return ok(-int64(syscall.EINVAL))
	}
	if a[2] == madvDontneed {
		p.mem.Zero(a[0], pageUp(a[1]))
	}
	return ok(0)
}

// limit is the (soft, hard) pair of resource r.
func limit(r uint64) (uint64, uint64) {
	switch r {
	case rlimitNofile:
		return maxFDs, maxFDs
	case rlimitStack:
		return StackSize, StackSize
	}
	return rlimInfinity, rlimInfinity
}

// prlimit64(pid, resource, new, old): only the caller (pid 0 or its own).
func sysPrlimit64Fn(p *Process, a [6]uint64) (int64, bool) {
	if a[0] != 0 && a[0] != uint64(p.PID) {
		return ok(-int64(syscall.EPERM))
	}
	return ok(p.rlimit(a[1], a[2], a[3]))
}

func (p *Process) rlimit(r, newVA, oldVA uint64) int64 {
	if r >= rlimitNlimit {
		return -int64(syscall.EINVAL)
	}
	soft, hard := limit(r)
	le := binary.LittleEndian
	if newVA != 0 {
		b := make([]byte, 16)
		if err := p.mem.CopyIn(newVA, b); err != nil {
			return errno(err)
		}
		if ns, nh := le.Uint64(b), le.Uint64(b[8:]); ns > nh || nh > hard {
			return -int64(syscall.EPERM) // the limits are fixed; asking for less is fine
		}
	}
	if oldVA != 0 {
		b := make([]byte, 16)
		le.PutUint64(b, soft)
		le.PutUint64(b[8:], hard)
		return errno(p.mem.CopyOut(oldVA, b))
	}
	return 0
}

func init() {
	table[28] = sysMadviseFn
	table[302] = sysPrlimit64Fn
	table[97] = func(p *Process, a [6]uint64) (int64, bool) { return ok(p.rlimit(a[0], 0, a[1])) }  // getrlimit
	table[160] = func(p *Process, a [6]uint64) (int64, bool) { return ok(p.rlimit(a[0], a[1], 0)) } // setrlimit
}
