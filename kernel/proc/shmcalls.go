package proc

// shmcalls.go - the calls over shared objects (W18-S03, shm.go): open and
// unlink under /dev/shm (shm_open, shm_unlink), memfd_create, and
// mmap(MAP_SHARED) of such an object or of anonymous memory. Never maps an
// object's frame copy-on-write. Docs: POSIX_COVERAGE.md.

import (
	"strings"
	"syscall"
)

const (
	mfdCloexec      = 1
	mfdAllowSealing = 2
	memfdNameMax    = 249 // as Linux: 255 less "memfd:"
)

// openShm opens /dev/shm/<name> (path is its io/fs path), creating it with
// O_CREAT (and failing with O_EXCL if it exists).
func (s *System) openShm(path string, flags int) (*file, error) {
	name := strings.TrimPrefix(path, shmPrefix)
	if strings.Contains(name, "/") {
		return nil, syscall.EINVAL
	}
	s.shmNames.mu.Lock()
	o := s.shmNames.m[name]
	switch {
	case o != nil && flags&oCreat != 0 && flags&oExcl != 0:
		s.shmNames.mu.Unlock()
		return nil, syscall.EEXIST
	case o == nil && flags&oCreat == 0:
		s.shmNames.mu.Unlock()
		return nil, syscall.ENOENT
	case o == nil:
		o = newShmObj(s.nu, name, 0)
		o.linked = true
		if s.shmNames.m == nil {
			s.shmNames.m = map[string]*shmObj{}
		}
		s.shmNames.m[name] = o
	}
	s.shmNames.mu.Unlock()
	if flags&oTrunc != 0 && flags&oAccMode != oRDONLY {
		o.truncate(0)
	}
	f := newFile(openShm(o), nil)
	f.path, f.flags = path, flags&(oAccMode|oNonblock)
	return f, nil
}

// unlinkShm removes /dev/shm/<name>; the object lives on while open or mapped.
func (s *System) unlinkShm(path string) int64 {
	name := strings.TrimPrefix(path, shmPrefix)
	s.shmNames.mu.Lock()
	o := s.shmNames.m[name]
	delete(s.shmNames.m, name)
	s.shmNames.mu.Unlock()
	if o == nil {
		return -int64(syscall.ENOENT)
	}
	o.mu.Lock()
	o.linked = false
	o.refs++ // dropped at once: frees the frames if nothing else holds it
	o.mu.Unlock()
	o.unref()
	return 0
}

// memfd_create(name, flags): an object with no name in any directory.
func sysMemfdCreateFn(p *Process, a [6]uint64) (int64, bool) {
	if a[1]&^(mfdCloexec|mfdAllowSealing) != 0 {
		return ok(-int64(syscall.EINVAL))
	}
	name, err := p.mem.CString(a[0], memfdNameMax+1)
	if err != nil {
		return ok(-int64(syscall.EINVAL))
	}
	f := newFile(openShm(newShmObj(p.sys.nu, "memfd:"+name, 0)), nil)
	f.flags = oRDWR
	n, err := p.fds.add(f, 0, a[1]&mfdCloexec != 0)
	if err != nil {
		f.unref()
		return ok(errno(err))
	}
	return ok(int64(n))
}

// mmapShared maps n bytes of o from offset off at addr (or where there is
// room), MAP_SHARED: every process mapping o sees the same frames.
func (p *Process) mmapShared(o *shmObj, off, n uint64, pr Prot, none, fixed bool, addr uint64) int64 {
	if off%PageSize != 0 || int64(off) < 0 {
		return -int64(syscall.EINVAL)
	}
	addr, e := p.pickAddr(addr, n, fixed)
	if e != 0 {
		return e
	}
	if err := p.mem.ReserveShared(addr, n, pr, none, o, int64(off)); err != nil {
		return -int64(syscall.ENOMEM)
	}
	return int64(addr)
}

func init() {
	table[319] = sysMemfdCreateFn
}
