package proc

// procfiles.go - what /proc shows of a process's files and memory (W23-S03):
// each descriptor's target (the path it was opened at, else pipe:[n],
// socket:[n], anon_inode:[...] as Linux names them; n is stable while the
// object lives), its position and flags; the address space as maps lines;
// the mount table; and who may look (the same user, or root). Never
// changes a process or a description. Docs: WAVE-23-proc-virtio.md.

import (
	"fmt"
	"io"
	"sort"
	"unsafe"

	"burrow/kernel/procfs"
	"burrow/kernel/vfs"
)

// objID is a small stable number for an object while it lives.
func objID(p unsafe.Pointer) uint64 { return uint64(uintptr(p)>>4) & 0xFFFFFFF }

// target is what /proc/<pid>/fd/N reads as for d.
func target(d *file) string {
	switch x := d.f.(type) {
	case *pipeEnd:
		return fmt.Sprintf("pipe:[%d]", objID(unsafe.Pointer(x.p)))
	case *sockEnd:
		return fmt.Sprintf("socket:[%d]", objID(unsafe.Pointer(x)))
	case *inetSock:
		return fmt.Sprintf("socket:[%d]", objID(unsafe.Pointer(x)))
	case *epoll:
		return "anon_inode:[eventpoll]"
	case *eventfd:
		return "anon_inode:[eventfd]"
	case ptmFile:
		return "/dev/ptmx"
	case *shmFile:
		if d.path == "" {
			return "/" + x.o.name + " (deleted)" // memfd_create's, as Linux shows it
		}
	}
	switch {
	case d.console != nil:
		return "/dev/console"
	case d.path == "" || d.path == ".":
		return "/"
	}
	return "/" + d.path
}

// snapshot is every descriptor in number order.
func (t *fdTable) snapshot() []procfs.FD {
	t.mu.Lock()
	out := make([]procfs.FD, 0, len(t.fds))
	files := make([]*file, 0, len(t.fds))
	for n, e := range t.fds {
		out = append(out, procfs.FD{Num: n, Target: target(e.file), Flags: e.file.flags})
		files = append(files, e.file)
	}
	t.mu.Unlock()
	for i, d := range files {
		if s, ok := d.f.(io.Seeker); ok && d.console == nil {
			out[i].Pos, _ = s.Seek(0, io.SeekCurrent)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Num < out[j].Num })
	return out
}

// perms is a maps line's "rwxp".
func perms(prot Prot, none, shared bool) string {
	b := []byte("r--p")
	switch {
	case none:
		b[0] = '-'
	default:
		if prot&ProtWrite != 0 {
			b[1] = 'w'
		}
		if prot&ProtExec != 0 {
			b[2] = 'x'
		}
	}
	if shared {
		b[3] = 's'
	}
	return string(b)
}

// maps is the address space as /proc/<pid>/maps lists it: the areas (the
// program's segments named after it, the heap, shared objects), the stack
// and device memory.
func (m *Mem) maps(exe string, brkStart, brk uint64) []procfs.Map {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []procfs.Map
	for _, a := range m.areas {
		e := procfs.Map{Start: a.start, End: a.end, Perms: perms(a.prot, a.none, a.shm != nil)}
		switch {
		case a.program:
			e.Name, e.Offset = exe, uint64(a.off)
		case a.shm != nil:
			e.Name, e.Offset = "/"+a.shm.name, uint64(a.off)
		case a.start >= brkStart && a.end <= pageUp(brk):
			e.Name = "[heap]"
		}
		out = append(out, e)
	}
	out = append(out, m.pageRuns(exe, brkStart)...)
	sort.Slice(out, func(i, j int) bool { return out[i].Start < out[j].Start })
	return out
}

// pageRuns are the pages outside any area: a program loaded whole (below
// the heap), the stack, and device memory (a framebuffer), each a run of
// neighbouring pages (m.mu held).
func (m *Mem) pageRuns(exe string, brkStart uint64) []procfs.Map {
	var vas []uint64
	for va := range m.pages {
		if _, in := m.areaAt(va); !in {
			vas = append(vas, va)
		}
	}
	sort.Slice(vas, func(i, j int) bool { return vas[i] < vas[j] })
	var out []procfs.Map
	for i := 0; i < len(vas); {
		j, pg := i, m.pages[vas[i]]
		for j+1 < len(vas) && vas[j+1] == vas[j]+PageSize && m.pages[vas[j+1]].device == pg.device && vas[j+1] != VdsoBase {
			j++
		}
		e := procfs.Map{Start: vas[i], End: vas[j] + PageSize, Perms: perms(pg.prot, false, pg.device)}
		switch {
		case pg.device && vdsoName(vas[i]) != "": // W25-S02: private, as Linux shows them
			e.Name, e.Perms = vdsoName(vas[i]), perms(pg.prot, false, false)
		case pg.device:
			e.Name = "/dev/fb0"
		case vas[i] >= StackTop-StackSize && vas[j] < StackTop:
			e.Name = "[stack]"
		case vas[j] < brkStart:
			e.Name = exe
		}
		out = append(out, e)
		i = j + 1
	}
	return out
}

// Mounts is the mount table as /proc/mounts lists it.
func (s *System) Mounts() []procfs.Mount {
	v, ok := s.files.(interface{ Mounts() []vfs.MountInfo })
	if !ok {
		return nil
	}
	var out []procfs.Mount
	for _, m := range v.Mounts() {
		out = append(out, procfs.Mount{Device: m.Kind, Point: m.Point, Kind: m.Kind, ReadOnly: m.ReadOnly})
	}
	return out
}

// MayTrace says whether c may see pid's descriptors and links: root, or
// a caller whose effective user is the target's real and effective one.
func (s *System) MayTrace(c vfs.Caller, pid int) bool {
	s.mu.Lock()
	from, to := s.byPID[c.PID], s.byPID[pid]
	s.mu.Unlock()
	if from == nil || to == nil {
		return false
	}
	fc, tc := from.Cred(), to.Cred()
	euid := fc.UID[idEff]
	return euid == 0 || euid == tc.UID[idReal] && euid == tc.UID[idEff]
}
