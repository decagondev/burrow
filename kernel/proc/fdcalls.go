package proc

import (
	"encoding/binary"
	"io/fs"
	"syscall"
	"time"
)

// Linux numbers of the descriptor and path calls (POSIX_COVERAGE.md).
func init() {
	for nr, h := range map[uint64]handler{
		4: sysStatFn, 6: sysStatFn, 21: sysAccessFn, 22: sysPipeFn, 32: sysDupFn, 33: sysDup2Fn,
		63: sysUnameFn, 72: sysFcntlFn, 79: sysGetcwdFn, 80: sysChdirFn, 81: sysFchdirFn,
		83: sysMkdirFn, 84: sysRmdirFn, 87: sysUnlinkFn, 217: sysGetdents64Fn, 258: sysMkdiratFn,
		262: sysNewfstatatFn, 263: sysUnlinkatFn, 269: sysFaccessatFn, 292: sysDup3Fn,
		293: sysPipe2Fn, 439: sysFaccessatFn,
	} {
		table[nr] = h
	}
}

func sysDupFn(p *Process, a [6]uint64) (int64, bool) {
	f := p.fds.get(a[0])
	if f == nil {
		return ok(-int64(syscall.EBADF))
	}
	n, err := p.fds.add(f.ref(), 0, false)
	if err != nil {
		f.unref()
		return ok(errno(err))
	}
	return ok(int64(n))
}

func sysDup2Fn(p *Process, a [6]uint64) (int64, bool) {
	if a[0] == a[1] { // dup2(fd, fd) only checks fd
		if p.fds.get(a[0]) == nil {
			return ok(-int64(syscall.EBADF))
		}
		return ok(int64(a[1]))
	}
	return ok(p.dup3(a[0], a[1], 0))
}

func sysDup3Fn(p *Process, a [6]uint64) (int64, bool) {
	if a[0] == a[1] {
		return ok(-int64(syscall.EINVAL))
	}
	return ok(p.dup3(a[0], a[1], int(a[2])))
}

func (p *Process) dup3(oldfd, newfd uint64, flags int) int64 {
	f := p.fds.get(oldfd)
	if f == nil {
		return -int64(syscall.EBADF)
	}
	if err := p.fds.put(int(int32(newfd)), f.ref(), flags&oCloexec != 0); err != nil {
		f.unref()
		return errno(err)
	}
	return int64(int32(newfd))
}

func sysPipeFn(p *Process, a [6]uint64) (int64, bool)  { return ok(p.pipe2(a[0], 0)) }
func sysPipe2Fn(p *Process, a [6]uint64) (int64, bool) { return ok(p.pipe2(a[0], int(a[1]))) }

// pipe2(int fds[2], flags): fds[0] reads, fds[1] writes.
func (p *Process) pipe2(fdsVA uint64, flags int) int64 {
	r, w := newPipe()
	rf, wf := newFile(r, nil), newFile(w, nil)
	rf.flags, wf.flags = oRDONLY, oWRONLY
	cloexec := flags&oCloexec != 0
	rn, err := p.fds.add(rf, 0, cloexec)
	if err != nil {
		rf.unref()
		wf.unref()
		return errno(err)
	}
	wn, err := p.fds.add(wf, 0, cloexec)
	if err != nil {
		p.fds.close(uint64(rn))
		wf.unref()
		return errno(err)
	}
	var b [8]byte
	binary.LittleEndian.PutUint32(b[0:], uint32(rn))
	binary.LittleEndian.PutUint32(b[4:], uint32(wn))
	if err := p.mem.CopyOut(fdsVA, b[:]); err != nil {
		p.fds.close(uint64(rn))
		p.fds.close(uint64(wn))
		return errno(err)
	}
	return 0
}

const (
	fDupfd        = 0
	fGetfd        = 1
	fSetfd        = 2
	fGetfl        = 3
	fSetfl        = 4
	fDupfdCloexec = 1030
	fdCloexec     = 1
	oStatusFlags  = oAppend | 0x800 /* O_NONBLOCK */
)

func sysFcntlFn(p *Process, a [6]uint64) (int64, bool) {
	f := p.fds.get(a[0])
	if f == nil {
		return ok(-int64(syscall.EBADF))
	}
	switch a[1] {
	case fDupfd, fDupfdCloexec:
		n, err := p.fds.add(f.ref(), int(int32(a[2])), a[1] == fDupfdCloexec)
		if err != nil {
			f.unref()
			return ok(errno(err))
		}
		return ok(int64(n))
	case fGetfd:
		if p.fds.cloexec(int(int32(a[0]))) {
			return ok(fdCloexec)
		}
		return ok(0)
	case fSetfd:
		p.fds.setCloexec(int(int32(a[0])), a[2]&fdCloexec != 0)
		return ok(0)
	case fGetfl:
		f.mu.Lock()
		defer f.mu.Unlock()
		return ok(int64(f.flags))
	case fSetfl:
		f.mu.Lock()
		f.flags = f.flags&^oStatusFlags | int(a[2])&oStatusFlags
		f.mu.Unlock()
		if w, isW := f.f.(*wfile); isW {
			w.mu.Lock()
			w.append = int(a[2])&oAppend != 0
			w.mu.Unlock()
		}
		return ok(0)
	}
	return ok(-int64(syscall.EINVAL))
}

// getdents64: struct linux_dirent64 { u64 ino; s64 off; u16 reclen; u8 type; char name[]; }
func sysGetdents64Fn(p *Process, a [6]uint64) (int64, bool) {
	f := p.fds.get(a[0])
	if f == nil || f.f == nil {
		return ok(-int64(syscall.EBADF))
	}
	rd, isDir := f.f.(fs.ReadDirFile)
	if !isDir {
		return ok(-int64(syscall.ENOTDIR))
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if !f.dirRead {
		ents, err := rd.ReadDir(-1)
		if err != nil {
			return ok(errno(err))
		}
		f.dirents = append([]fs.DirEntry{dotEntry("."), dotEntry("..")}, ents...)
		f.dirRead = true
	}
	var out []byte
	for len(f.dirents) > 0 {
		e := f.dirents[0]
		rec := 19 + len(e.Name()) + 1
		rec = (rec + 7) &^ 7
		if uint64(len(out)+rec) > a[2] {
			break
		}
		b := make([]byte, rec)
		binary.LittleEndian.PutUint64(b[0:], uint64(len(out)+1)|1<<40) // a made-up inode
		binary.LittleEndian.PutUint64(b[8:], uint64(len(out)+rec))
		binary.LittleEndian.PutUint16(b[16:], uint16(rec))
		b[18] = direntType(e.Type())
		copy(b[19:], e.Name())
		out = append(out, b...)
		f.dirents = f.dirents[1:]
	}
	if len(out) == 0 && len(f.dirents) > 0 {
		return ok(-int64(syscall.EINVAL)) // buffer smaller than one entry
	}
	if err := p.mem.CopyOut(a[1], out); err != nil {
		return ok(errno(err))
	}
	return ok(int64(len(out)))
}

func direntType(m fs.FileMode) byte {
	switch {
	case m.IsDir():
		return 4 // DT_DIR
	case m&fs.ModeCharDevice != 0:
		return 2 // DT_CHR
	case m&fs.ModeNamedPipe != 0:
		return 1 // DT_FIFO
	case m.IsRegular():
		return 8 // DT_REG
	}
	return 0
}

type dotInfo string

func (d dotInfo) Name() string       { return string(d) }
func (d dotInfo) Size() int64        { return 0 }
func (d dotInfo) Mode() fs.FileMode  { return fs.ModeDir | 0o755 }
func (d dotInfo) ModTime() time.Time { return time.Time{} }
func (d dotInfo) IsDir() bool        { return true }
func (d dotInfo) Sys() any           { return nil }

func dotEntry(name string) fs.DirEntry { return fs.FileInfoToDirEntry(dotInfo(name)) }

// consoleInfo is what fstat says about fds 0-2 before the TTY.
type consoleInfo struct{}

func (consoleInfo) Name() string       { return "console" }
func (consoleInfo) Size() int64        { return 0 }
func (consoleInfo) Mode() fs.FileMode  { return fs.ModeDevice | fs.ModeCharDevice | 0o620 }
func (consoleInfo) ModTime() time.Time { return time.Time{} }
func (consoleInfo) IsDir() bool        { return false }
func (consoleInfo) Sys() any           { return nil }

// uname: struct utsname, six 65-byte strings.
func sysUnameFn(p *Process, a [6]uint64) (int64, bool) {
	b := make([]byte, 6*65)
	for i, s := range []string{"Burrow", "burrow", "0.10", "#1 Wave 10", "x86_64", "(none)"} {
		copy(b[i*65:], s)
	}
	return ok(errno(p.mem.CopyOut(a[0], b)))
}
