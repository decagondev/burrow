package proc

// robust.go - set_robust_list(2) (W24-S02: musl's fork calls it in the
// child, so every forked C program does; dropbear's census showed it). The
// head is recorded per thread and its length checked as Linux does; the
// list is not yet walked when a thread dies, so a robust mutex its owner
// held at its death is not marked FUTEX_OWNER_DIED. get_robust_list is not
// served. Docs: docs/02-architecture/POSIX_COVERAGE.md.

import "syscall"

const robustListHeadBytes = 24 // struct robust_list_head: list, futex_offset, list_op_pending

// set_robust_list(head, len).
func sysSetRobustListFn(t *Thread, a [6]uint64) (int64, bool) {
	if a[1] != robustListHeadBytes {
		return ok(-int64(syscall.EINVAL))
	}
	t.p.mu.Lock()
	t.robustList = a[0]
	t.p.mu.Unlock()
	return ok(0)
}

func init() { ttable[273] = sysSetRobustListFn }
