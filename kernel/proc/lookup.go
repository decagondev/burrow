package proc

// lookup.go - paths from user space (W19-S05): every one is joined to its
// starting directory as text (the cwd, or a directory descriptor's path)
// and handed to the VFS's walker (vfs.Resolve), which follows symbolic
// links, takes ".." from where a link led, and calls back for every
// directory on the way, where search permission is checked with the
// caller's credentials. What comes back is a link-free path every other
// call uses as it is. A tree without the walker (tests over a plain
// fs.FS) falls back to cleaning the text, as before W19. Never lets a path
// past a directory the caller may not search. Docs: WAVE-19-unix.md.

import (
	"errors"
	"io/fs"
	"strings"
	"syscall"

	"burrow/kernel/vfs"
)

const oNofollow = 0o400000

// resolver is the VFS's walker; links that depend on the caller (/proc/self,
// W23-S01) read as the walking process's.
type resolver interface {
	ResolveAs(raw string, follow bool, c vfs.Caller, visit func(dir string, info fs.FileInfo) error) (string, error)
}

// caller is p as the VFS knows it.
func (p *Process) caller() vfs.Caller { return vfs.Caller{PID: p.PID, TID: p.PID} }

// rawPath is name as the walker takes it: from the root for an absolute
// name, else after the cwd (AT_FDCWD) or the directory open at dirfd.
func (p *Process) rawPath(dirfd uint64, name string) (string, error) {
	switch {
	case name == "":
		return "", syscall.ENOENT
	case strings.HasPrefix(name, "/"):
		return name, nil
	case int32(dirfd) == atFDCWD:
		p.mu.Lock()
		defer p.mu.Unlock()
		return p.cwd + "/" + name, nil
	}
	d := p.fds.get(dirfd)
	if d == nil || d.f == nil || d.path == "" {
		return "", syscall.EBADF
	}
	return d.path + "/" + name, nil
}

// lookupAs resolves raw as c may (search permission on every directory),
// following a final link when follow is set.
func (p *Process) lookupAs(c *Cred, raw string, follow bool) (string, error) {
	if name := cleanPath(raw); isShmPath(name) || isPTYPath(name) {
		return name, nil // /dev/shm and /dev/pts are tables, not directories of the tree
	}
	r, ok := p.sys.files.(resolver)
	if !ok {
		name := cleanPath(raw)
		return name, p.search(c, name)
	}
	return r.ResolveAs(raw, follow, p.caller(), func(_ string, info fs.FileInfo) error {
		if !c.allows(info, mayExec) {
			return syscall.EACCES
		}
		return nil
	})
}

// atFollow resolves the name at nameVA for an *at call.
func (p *Process) atFollow(dirfd, nameVA uint64, follow bool) (string, error) {
	name, err := p.mem.CString(nameVA, 4096)
	if err != nil {
		return "", err
	}
	raw, err := p.rawPath(dirfd, name)
	if err != nil {
		return "", err
	}
	c := p.Cred()
	return p.lookupAs(&c, raw, follow)
}

// at resolves a name, following a final link (open, stat, chdir, ...).
func (p *Process) at(dirfd, nameVA uint64) (string, error) { return p.atFollow(dirfd, nameVA, true) }

// atNoFollow resolves a name whose last part is the name itself (unlink,
// rename, mkdir, lstat, readlink, symlink, ...).
func (p *Process) atNoFollow(dirfd, nameVA uint64) (string, error) {
	return p.atFollow(dirfd, nameVA, false)
}

// resolvePath resolves a name the kernel holds already (execve's program,
// a script's interpreter, a socket's path), from the cwd.
func (p *Process) resolvePath(name string, follow bool) (string, error) {
	raw, err := p.rawPath(atFDCWDArg, name)
	if err != nil {
		return "", err
	}
	c := p.Cred()
	return p.lookupAs(&c, raw, follow)
}

// statPath describes name, a final link itself unless follow is set.
func (p *Process) statPath(name string, follow bool) (fs.FileInfo, error) {
	if isPTYPath(name) { // a table, not the tree (procfd.go, W23-S03)
		return p.sys.ptyInfo(name)
	}
	if rl, ok := p.sys.files.(fs.ReadLinkFS); ok && !follow {
		return rl.Lstat(name)
	}
	return fs.Stat(p.sys.files, name)
}

func isLink(info fs.FileInfo) bool { return info != nil && info.Mode()&fs.ModeSymlink != 0 }

func notExist(err error) bool { return errors.Is(err, fs.ErrNotExist) }
