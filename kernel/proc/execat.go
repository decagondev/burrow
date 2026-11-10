package proc

// execat.go - execveat(2) (W24-S02: dropbear runs itself again for each
// connection from the descriptor of its own program, AT_EMPTY_PATH, and
// only falls back without it): a path relative to a directory descriptor,
// or with AT_EMPTY_PATH and an empty path the file a descriptor was opened
// at; AT_SYMLINK_NOFOLLOW refuses a final link (ELOOP). The rest is
// execve's (exec.go). A descriptor with no path (a pipe, a memfd) cannot be
// run (EACCES). Docs: docs/02-architecture/POSIX_COVERAGE.md.

import "syscall"

// execveat(dirfd, path, argv, envp, flags).
func sysExecveatFn(t *Thread, a [6]uint64) (int64, bool) {
	p := t.p
	flags := a[4]
	if flags&^(atEmptyPath|atSymlinkNofollw) != 0 {
		return ok(-int64(syscall.EINVAL))
	}
	name, err := p.mem.CString(a[1], 4096)
	if err != nil {
		return ok(errno(err))
	}
	raw := name
	switch {
	case name == "" && flags&atEmptyPath != 0:
		f := p.fds.get(a[0])
		if f == nil {
			return ok(-int64(syscall.EBADF))
		}
		if f.path == "" || f.console != nil {
			return ok(-int64(syscall.EACCES))
		}
		raw = "/" + f.path
	default:
		if raw, err = p.rawPath(a[0], name); err != nil {
			return ok(errno(err))
		}
	}
	return ok(t.execName(raw, flags&atSymlinkNofollw == 0, a[2], a[3]))
}

func init() { ttable[322] = sysExecveatFn }
