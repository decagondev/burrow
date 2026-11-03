package proc_test

import (
	"bytes"
	"strings"
	"sync"
	"syscall"
	"testing"
	"testing/fstest"
	"time"

	"burrow/kernel/klog"
	"burrow/kernel/proc"
	"burrow/kernel/proc/proctest"
)

// syncBuf is a bytes.Buffer safe for the dispatcher's goroutines.
type syncBuf struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuf) Write(p []byte) (int, error) { s.mu.Lock(); defer s.mu.Unlock(); return s.b.Write(p) }
func (s *syncBuf) String() string              { s.mu.Lock(); defer s.mu.Unlock(); return s.b.String() }

type rig struct {
	t       *testing.T
	nu      *proctest.Fake
	sys     *proc.System
	console *syncBuf
	log     *syncBuf
}

func program() []byte {
	f := buildELF(2, 0x400200, []seg{{vaddr: 0x400000, off: 0, memsz: 0x1000, flags: pfR | pfX, data: make([]byte, 0x200)}})
	return f
}

func newRig(t *testing.T) *rig {
	nu := proctest.New()
	r := &rig{t: t, nu: nu, console: &syncBuf{}, log: &syncBuf{}}
	files := fstest.MapFS{
		"bin/prog": {Data: program(), Mode: 0o755},
		"etc/motd": {Data: []byte("hello file\n"), Mode: 0o644},
		"etc/text": {Data: []byte("#!/bin/sh\n"), Mode: 0o755},
	}
	r.sys = proc.NewSystem(nu, files, r.console, klog.New(r.log, klog.Info))
	go r.sys.Serve()
	return r
}

func (r *rig) spawn() *proc.Process {
	p, err := r.sys.Spawn("bin/prog", nil, nil)
	if err != nil {
		r.t.Fatal(err)
	}
	return p
}

// call makes p's thread issue a syscall and returns the reply.
func (r *rig) call(p *proc.Process, nr uint64, args ...uint64) int64 {
	r.t.Helper()
	var a [6]uint64
	copy(a[:], args)
	r.nu.Inject(proc.Request{TID: p.TID(), Nr: nr, Args: a})
	select {
	case v := <-r.nu.Thread(p.TID()).Replies:
		return v
	case <-time.After(2 * time.Second):
		r.t.Fatalf("no reply to syscall %d", nr)
		return 0
	}
}

// scratch is user memory for staging arguments: the bottom of the stack.
const scratch = proc.StackTop - proc.StackSize

func (r *rig) put(p *proc.Process, va uint64, b []byte) {
	if err := p.Mem().CopyOut(va, b); err != nil {
		r.t.Fatal(err)
	}
}

func wait(t *testing.T, p *proc.Process) (int, string) {
	t.Helper()
	ch := make(chan struct{})
	var st int
	var why string
	go func() { st, why = p.Wait(); close(ch) }()
	select {
	case <-ch:
	case <-time.After(2 * time.Second):
		t.Fatal("process did not end")
	}
	return st, why
}

func TestHelloAndExit(t *testing.T) {
	r := newRig(t)
	p := r.spawn()
	if th := r.nu.Thread(p.TID()); th.RIP != 0x400200 || th.RSP%16 != 0 || th.RSP > proc.StackTop {
		t.Errorf("thread starts at %#x with rsp %#x", th.RIP, th.RSP)
	}
	r.put(p, scratch, []byte("hello, "))
	r.put(p, scratch+64, []byte("ring 3\n"))
	if n := r.call(p, 1, 1, scratch, 7); n != 7 {
		t.Errorf("write = %d", n)
	}
	iov := make([]byte, 16)
	le.PutUint64(iov, scratch+64)
	le.PutUint64(iov[8:], 7)
	r.put(p, scratch+128, iov)
	if n := r.call(p, 20, 2, scratch+128, 1); n != 7 {
		t.Errorf("writev = %d", n)
	}
	if r.console.String() != "hello, ring 3\n" {
		t.Errorf("console %q", r.console.String())
	}
	if pid := r.call(p, 39); pid != int64(p.PID) {
		t.Errorf("getpid = %d", pid)
	}
	r.nu.Inject(proc.Request{TID: p.TID(), Nr: 231, Args: [6]uint64{3}})
	if st, why := wait(t, p); st != 3 || why != "" {
		t.Errorf("exit status %d %q", st, why)
	}
	if !r.nu.Thread(p.TID()).Killed || r.nu.FramesInUse() != 0 {
		t.Errorf("after exit: killed=%v, %d frames", r.nu.Thread(p.TID()).Killed, r.nu.FramesInUse())
	}
}

func TestFaultKillsOnlyThatProcess(t *testing.T) {
	r := newRig(t)
	a, b := r.spawn(), r.spawn()
	r.nu.Inject(proc.Request{TID: a.TID(), Nr: proc.FaultBase | 14, Args: [6]uint64{0, 0x400207, 4}})
	st, why := wait(t, a)
	if st != 139 || !strings.Contains(why, "page fault at 0x0") {
		t.Errorf("status %d %q", st, why)
	}
	if !strings.Contains(r.log.String(), "pid 2 (prog) killed: page fault at 0x0 (rip 0x400207)") {
		t.Errorf("log %q", r.log.String())
	}
	if pid := r.call(b, 39); pid != int64(b.PID) {
		t.Errorf("the other process answers getpid = %d", pid)
	}
}

func TestFiles(t *testing.T) {
	r := newRig(t)
	p := r.spawn()
	r.put(p, scratch, []byte("/etc/motd\x00"))
	fd := r.call(p, 2, scratch, 0)
	if fd != 3 {
		t.Fatalf("open = %d", fd)
	}
	if n := r.call(p, 0, uint64(fd), scratch+256, 100); n != 11 {
		t.Errorf("read = %d", n)
	}
	got := make([]byte, 11)
	p.Mem().CopyIn(scratch+256, got)
	if string(got) != "hello file\n" {
		t.Errorf("read %q", got)
	}
	if n := r.call(p, 0, uint64(fd), scratch+256, 100); n != 0 {
		t.Errorf("read at EOF = %d", n)
	}
	if off := r.call(p, 8, uint64(fd), 6, 0); off != 6 {
		t.Errorf("lseek = %d", off)
	}
	if r.call(p, 5, uint64(fd), scratch+512) != 0 {
		t.Error("fstat failed")
	}
	st := make([]byte, 144)
	p.Mem().CopyIn(scratch+512, st)
	if size, mode := le.Uint64(st[48:]), le.Uint32(st[24:]); size != 11 || mode != syscall.S_IFREG|0o644 {
		t.Errorf("fstat size %d mode %o", size, mode)
	}
	for _, c := range []struct {
		got, want int64
	}{
		{r.call(p, 3, uint64(fd)), 0},
		{r.call(p, 3, uint64(fd)), -int64(syscall.EBADF)},
		{r.call(p, 2, scratch, 1), -int64(syscall.EROFS)},
		{r.call(p, 0, 9, scratch, 1), -int64(syscall.EBADF)},
		{r.call(p, 1, 1, 0x10, 5), -int64(syscall.EFAULT)},
		{r.call(p, 16, 1, 0x5401, 0), -int64(syscall.ENOTTY)},
		{r.call(p, 257, uint64(0xFFFFFF9C), scratch, 0), 3}, // openat(AT_FDCWD, "/etc/motd")
	} {
		if c.got != c.want {
			t.Errorf("got %d, want %d", c.got, c.want)
		}
	}
	r.put(p, scratch, []byte("nope\x00"))
	if e := r.call(p, 2, scratch, 0); e != -int64(syscall.ENOENT) {
		t.Errorf("open missing = %d", e)
	}
}

func TestMemoryCalls(t *testing.T) {
	r := newRig(t)
	p := r.spawn()
	start := uint64(r.call(p, 12, 0))
	if start != 0x401000 {
		t.Fatalf("brk(0) = %#x", start)
	}
	if got := uint64(r.call(p, 12, start+10000)); got != start+10000 || !p.Mem().Mapped(start, 10000) {
		t.Errorf("brk grow = %#x", got)
	}
	if got := uint64(r.call(p, 12, 0x10)); got != start+10000 {
		t.Errorf("brk below its start = %#x, want unchanged", got)
	}
	addr := uint64(r.call(p, 9, 0, 8192, 3, 0x22, ^uint64(0), 0))
	if addr < 0x1000_0000_0000 || !p.Mem().Mapped(addr, 8192) {
		t.Fatalf("mmap = %#x", addr)
	}
	r.put(p, addr, []byte("anonymous"))
	if r.call(p, 11, addr, 8192) != 0 || p.Mem().Mapped(addr, 1) {
		t.Error("munmap")
	}
	if e := r.call(p, 9, 0, 4096, 1, 0x02, 3, 0); e != -int64(syscall.EBADF) {
		t.Errorf("mmap of a closed fd = %d", e)
	}
	r.str(p, scratch, "/etc/motd")
	fd := uint64(r.call(p, 2, scratch, 0))
	if e := r.call(p, 9, 0, 4096, 1, 0x02, fd, 0); e != -int64(syscall.ENODEV) {
		t.Errorf("mmap of a regular file = %d, want ENODEV (not mappable yet)", e)
	}
	fixed := uint64(0x2000_0000_0000)
	if got := uint64(r.call(p, 9, fixed, 4096, 3, 0x32, ^uint64(0), 0)); got != fixed {
		t.Errorf("MAP_FIXED = %#x", got)
	}
}

func TestArchPrctlAndUnknown(t *testing.T) {
	r := newRig(t)
	p := r.spawn()
	if r.call(p, 158, 0x1002, 0x7000) != 0 || r.nu.Thread(p.TID()).FS != 0x7000 {
		t.Error("ARCH_SET_FS")
	}
	r.call(p, 158, 0x1003, scratch)
	b := make([]byte, 8)
	p.Mem().CopyIn(scratch, b)
	if le.Uint64(b) != 0x7000 {
		t.Errorf("ARCH_GET_FS = %#x", le.Uint64(b))
	}
	for i := 0; i < 2; i++ {
		if e := r.call(p, 999); e != -int64(syscall.ENOSYS) {
			t.Errorf("unknown = %d", e)
		}
	}
	if n := strings.Count(r.log.String(), "unimplemented syscall 999"); n != 1 {
		t.Errorf("logged %d times, want once", n)
	}
}

func TestSpawnErrors(t *testing.T) {
	r := newRig(t)
	for _, f := range []string{"bin/none", "etc/text"} {
		if _, err := r.sys.Spawn(f, nil, nil); err == nil {
			t.Errorf("Spawn(%s) succeeded", f)
		}
	}
	if r.nu.FramesInUse() != 0 {
		t.Errorf("a failed spawn leaked %d frames", r.nu.FramesInUse())
	}
}

func TestReadv(t *testing.T) {
	r := newRig(t)
	p := r.spawn()
	r.str(p, scratch, "/etc/motd")
	fd := uint64(r.call(p, 2, scratch, 0))
	iov := make([]byte, 32)
	le.PutUint64(iov, scratch+256)
	le.PutUint64(iov[8:], 5)
	le.PutUint64(iov[16:], scratch+512)
	le.PutUint64(iov[24:], 100)
	r.put(p, scratch+128, iov)
	if n := r.call(p, 19, fd, scratch+128, 2); n != 11 {
		t.Fatalf("readv = %d, want 11", n)
	}
	a, b := make([]byte, 5), make([]byte, 6)
	p.Mem().CopyIn(scratch+256, a)
	p.Mem().CopyIn(scratch+512, b)
	if string(a)+string(b) != "hello file\n" {
		t.Errorf("readv scattered %q + %q", a, b)
	}
	if e := r.call(p, 19, 99, scratch+128, 2); e != -int64(syscall.EBADF) {
		t.Errorf("readv of a bad fd = %d", e)
	}
}

func TestMprotect(t *testing.T) {
	r := newRig(t)
	p := r.spawn()
	addr := uint64(r.call(p, 9, 0, 3*4096, 0, 0x22, ^uint64(0), 0)) // PROT_NONE
	if p.Mem().CopyOut(addr+4096, []byte{1}) == nil {
		t.Error("PROT_NONE memory was writable")
	}
	if r.call(p, 10, addr+4096, 8192, 3) != 0 {
		t.Fatal("mprotect")
	}
	if err := p.Mem().CopyOut(addr+4096, []byte{1}); err != nil {
		t.Errorf("after mprotect(RW): %v", err)
	}
	if pr, _ := r.nu.Prot(p.Mem().Space(), addr+4096); pr != proc.ProtWrite {
		t.Errorf("the touched page maps with prot %v", pr)
	}
	if p.Mem().CopyIn(addr, make([]byte, 1)) == nil {
		t.Error("the guard page became accessible")
	}
	if e := r.call(p, 10, addr+3*4096, 4096, 3); e != -int64(syscall.ENOMEM) {
		t.Errorf("mprotect of unmapped memory = %d", e)
	}
	if e := r.call(p, 10, addr+1, 4096, 3); e != -int64(syscall.EINVAL) {
		t.Errorf("unaligned mprotect = %d", e)
	}
}

// mmap reserves; a page gets its frame at first touch, a fault the thread
// resumes from. PROT_NONE and writes to read-only areas are SIGSEGV.
func TestDemandZero(t *testing.T) {
	r := newRig(t)
	p := r.spawn()
	before := r.nu.FramesInUse()
	big := uint64(r.call(p, 9, 0, 1<<40, 3, 0x22, ^uint64(0), 0)) // 1 TiB, as Go reserves
	if int64(big) < 0 || r.nu.FramesInUse() != before {
		t.Fatalf("mmap of 1 TiB = %d, %d frames", int64(big), r.nu.FramesInUse()-before)
	}
	r.nu.Inject(proc.Request{TID: p.TID(), Nr: proc.FaultBase | 14, Args: [6]uint64{big + 5000, 0x400200, 2}}) // write, not present
	if v := r.await(p.TID()); v != 0 || r.nu.FramesInUse() != before+1 {
		t.Fatalf("first touch: reply %d, %d new frames", v, r.nu.FramesInUse()-before)
	}
	if pa, ok := r.nu.PA(p.Mem().Space(), big+4096); !ok || pa == 0 {
		t.Error("the touched page is not mapped")
	}
	kid := r.call(p, 57) // fork copies the areas and the backed page
	_ = kid
	ro := uint64(r.call(p, 9, 0, 4096, 1, 0x22, ^uint64(0), 0)) // PROT_READ
	r.nu.Inject(proc.Request{TID: p.TID(), Nr: proc.FaultBase | 14, Args: [6]uint64{ro, 0x400200, 2}})
	if st, why := wait(t, p); st != 139 || why == "" {
		t.Errorf("write to a read-only area: status %d %q", st, why)
	}
}

// A first touch the nucleus has no frame for kills the process (SIGKILL,
// logged as out of memory) and gives its frames back (W14-S01).
func TestOutOfMemoryKills(t *testing.T) {
	r := newRig(t)
	p := r.spawn()
	big := uint64(r.call(p, 9, 0, 1<<30, 3, 0x22, ^uint64(0), 0))
	r.nu.LimitFrames(r.nu.FramesInUse())
	r.nu.Inject(proc.Request{TID: p.TID(), Nr: proc.FaultBase | 14, Args: [6]uint64{big, 0x400200, 2}})
	st, why := wait(t, p)
	if st != 128+9 || !strings.Contains(why, "out of memory") {
		t.Errorf("status %d %q, want 137 and out of memory", st, why)
	}
	r.nu.LimitFrames(0)
	if n := r.nu.FramesInUse(); n != 0 {
		t.Errorf("%d frames still in use after the kill", n)
	}
}
