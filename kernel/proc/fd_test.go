package proc_test

import (
	"bytes"
	"io/fs"
	"strings"
	"syscall"
	"testing"
	"time"

	"burrow/kernel/fs/memfs"
	"burrow/kernel/klog"
	"burrow/kernel/proc"
	"burrow/kernel/proc/proctest"
	"burrow/kernel/vfs"
)

// vfsRig is newRig over the real VFS: a frozen root and a writable /tmp.
func vfsRig(t *testing.T) (*rig, *vfs.VFS) {
	root := memfs.New()
	root.MkdirAll("bin", 0o755, time.Time{})
	root.MkdirAll("etc", 0o755, time.Time{})
	root.WriteFile("bin/prog", program(), 0o755, time.Time{})
	root.WriteFile("etc/motd", []byte("hello file\n"), 0o644, time.Time{})
	root.Freeze()
	v := vfs.New()
	v.Mount(".", root, "memfs")
	v.Mount("tmp", memfs.New(), "ramfs")
	nu := proctest.New()
	r := &rig{t: t, nu: nu, console: &syncBuf{}, log: &syncBuf{}}
	r.sys = proc.NewSystem(nu, v, r.console, klog.New(r.log, klog.Info))
	go r.sys.Serve()
	return r, v
}

// str stages a NUL-terminated string at va.
func (r *rig) str(p *proc.Process, va uint64, s string) uint64 {
	r.put(p, va, append([]byte(s), 0))
	return va
}

func TestPipeBetweenProcesses(t *testing.T) {
	r, _ := vfsRig(t)
	p := r.spawn()
	if r.call(p, 293, scratch, 0) != 0 { // pipe2
		t.Fatal("pipe2")
	}
	b := make([]byte, 8)
	p.Mem().CopyIn(scratch, b)
	rd, wr := uint64(le.Uint32(b)), uint64(le.Uint32(b[4:]))
	if rd != 3 || wr != 4 {
		t.Fatalf("pipe fds %d %d", rd, wr)
	}
	r.call(p, 57) // fork: the child writes, the parent reads
	kid := r.newestThread(p.TID())
	r.nu.Inject(proc.Request{TID: p.TID(), Nr: 0, Args: [6]uint64{rd, scratch + 64, 100}})
	time.Sleep(10 * time.Millisecond) // the read blocks: nothing written yet
	if r.callAs(kid, 3, rd) != 0 {    // the child closes its read end
		t.Fatal("child close")
	}
	// the child's copy of scratch holds the two fds: write those 5 bytes
	if n := r.callAs(kid, 1, wr, scratch, 5); n != 5 {
		t.Fatalf("child write = %d", n)
	}
	if n := r.await(p.TID()); n != 5 {
		t.Fatalf("parent read = %d", n)
	}
	got := make([]byte, 5)
	p.Mem().CopyIn(scratch+64, got)
	if !bytes.Equal(got, b[:5]) {
		t.Errorf("read % x, want % x", got, b[:5])
	}
}

func TestPipeEOFAndEPIPE(t *testing.T) {
	r, _ := vfsRig(t)
	p := r.spawn()
	r.call(p, 22, scratch)
	if r.call(p, 1, 4, scratch, 3) != 3 || r.call(p, 3, 4) != 0 {
		t.Fatal("write, close")
	}
	if n := r.call(p, 0, 3, scratch+64, 10); n != 3 {
		t.Errorf("read buffered = %d", n)
	}
	if n := r.call(p, 0, 3, scratch+64, 10); n != 0 {
		t.Errorf("read after the last writer closed = %d, want 0 (EOF)", n)
	}
	r.call(p, 22, scratch)
	r.call(p, 3, 4)                      // the new pipe is 4 (read) and 5 (write): close the read end
	r.call(p, 13, 13, r.sigIgn(p), 0, 8) // SIGPIPE ignored: the write says EPIPE
	if e := r.call(p, 1, 5, scratch, 1); e != -int64(syscall.EPIPE) {
		t.Errorf("write with no reader = %d", e)
	}
}

// BUG-0015: a write to a pipe with no reader raises SIGPIPE, whose default
// action ends the writer (status 128 + 13, as `yes | head` needs).
func TestBUG0015PipeSIGPIPE(t *testing.T) {
	r, _ := vfsRig(t)
	p := r.spawn()
	r.call(p, 22, scratch)
	r.call(p, 3, 3) // close the read end
	r.nu.Inject(proc.Request{TID: p.TID(), Nr: 1, Args: [6]uint64{4, scratch, 1}})
	if st, _ := wait(t, p); st != 128+13 {
		t.Errorf("writer's status %d, want 141 (SIGPIPE)", st)
	}
}

func TestDupAndFcntl(t *testing.T) {
	r, _ := vfsRig(t)
	p := r.spawn()
	if fd := r.call(p, 32, 1); fd != 3 {
		t.Errorf("dup(1) = %d", fd)
	}
	if fd := r.call(p, 33, 1, 9); fd != 9 {
		t.Errorf("dup2(1, 9) = %d", fd)
	}
	r.str(p, scratch, "via fd 9")
	r.call(p, 1, 9, scratch, 8)
	if r.console.String() != "via fd 9" {
		t.Errorf("console %q", r.console.String())
	}
	if fd := r.call(p, 72, 1, 0, 20); fd != 20 {
		t.Errorf("F_DUPFD from 20 = %d", fd)
	}
	r.call(p, 72, 20, 2, 1)
	if r.call(p, 72, 20, 1) != 1 || r.call(p, 72, 9, 1) != 0 {
		t.Error("FD_CLOEXEC")
	}
	if e := r.call(p, 33, 77, 5); e != -int64(syscall.EBADF) {
		t.Errorf("dup2 of a closed fd = %d", e)
	}
	r.call(p, 59, r.str(p, scratch, "/bin/prog"), 0, 0) // execve: fd 20 goes, 9 stays
	if r.call(p, 5, 20, scratch) != -int64(syscall.EBADF) || r.call(p, 5, 9, scratch) != 0 {
		t.Error("close-on-exec")
	}
}

func TestCwdFilesAndDirectories(t *testing.T) {
	r, v := vfsRig(t)
	p := r.spawn()
	cwd := func() string {
		n := r.call(p, 79, scratch+512, 256)
		s, _ := p.Mem().CString(scratch+512, int(n))
		return s
	}
	if cwd() != "/" {
		t.Errorf("cwd %q", cwd())
	}
	if r.call(p, 83, r.str(p, scratch, "/tmp/d"), 0o755) != 0 || r.call(p, 80, r.str(p, scratch, "tmp/d")) != 0 {
		t.Fatal("mkdir, chdir")
	}
	if cwd() != "/tmp/d" {
		t.Errorf("cwd %q", cwd())
	}
	fd := r.call(p, 2, r.str(p, scratch, "f"), 0x241, 0o644) // O_WRONLY|O_CREAT|O_TRUNC
	r.str(p, scratch+64, "one ")
	r.call(p, 1, uint64(fd), scratch+64, 4)
	r.call(p, 3, uint64(fd))
	fd = r.call(p, 2, r.str(p, scratch, "f"), 0x441) // O_WRONLY|O_CREAT|O_APPEND
	r.str(p, scratch+64, "two")
	r.call(p, 1, uint64(fd), scratch+64, 3)
	if b, err := fs.ReadFile(v, "tmp/d/f"); err != nil || string(b) != "one two" {
		t.Errorf("tmp/d/f = %q, %v", b, err)
	}
	if e := r.call(p, 80, r.str(p, scratch, "f")); e != -int64(syscall.ENOTDIR) {
		t.Errorf("chdir to a file = %d", e)
	}
	for _, c := range []struct {
		got, want int64
	}{
		{r.call(p, 84, r.str(p, scratch, "/tmp/d")), -int64(syscall.ENOTEMPTY)},
		{r.call(p, 87, r.str(p, scratch, "/tmp/d")), -int64(syscall.EISDIR)},
		{r.call(p, 87, r.str(p, scratch, "f")), 0},
		{r.call(p, 21, r.str(p, scratch, "f"), 0), -int64(syscall.ENOENT)},
		{r.call(p, 84, r.str(p, scratch, "/tmp/d")), 0},
		{r.call(p, 2, r.str(p, scratch, "/etc/new"), 0x41), -int64(syscall.EACCES)}, // the frozen root
		{r.call(p, 2, r.str(p, scratch, "/etc/motd"), 0x10000), -int64(syscall.ENOTDIR)},
	} {
		if c.got != c.want {
			t.Errorf("got %d, want %d", c.got, c.want)
		}
	}
}

func TestStatAndGetdents(t *testing.T) {
	r, _ := vfsRig(t)
	p := r.spawn()
	if r.call(p, 262, uint64(0xFFFFFF9C), r.str(p, scratch, "etc/motd"), scratch+256, 0) != 0 {
		t.Fatal("newfstatat")
	}
	st := make([]byte, 144)
	p.Mem().CopyIn(scratch+256, st)
	if le.Uint64(st[48:]) != 11 || le.Uint32(st[24:])&syscall.S_IFMT != syscall.S_IFREG {
		t.Errorf("stat size %d mode %o", le.Uint64(st[48:]), le.Uint32(st[24:]))
	}
	dir := r.call(p, 257, uint64(0xFFFFFF9C), r.str(p, scratch, "/"), 0x10000)
	var names []string
	for {
		n := r.call(p, 217, uint64(dir), scratch+1024, 48) // small buffer: several calls
		if n <= 0 {
			if n < 0 {
				t.Fatalf("getdents64 = %d", n)
			}
			break
		}
		buf := make([]byte, n)
		p.Mem().CopyIn(scratch+1024, buf)
		for off := 0; off < len(buf); {
			rec := int(le.Uint16(buf[off+16:]))
			name := string(bytes.TrimRight(buf[off+19:off+rec], "\x00"))
			names = append(names, name+"/"+string('0'+buf[off+18]))
			off += rec
		}
	}
	if got := strings.Join(names, " "); got != "./4 ../4 bin/4 etc/4 tmp/4" {
		t.Errorf("entries %q", got)
	}
	if r.call(p, 63, scratch) != 0 {
		t.Error("uname")
	}
	if s, _ := p.Mem().CString(scratch, 65); s != "Burrow" {
		t.Errorf("uname sysname %q", s)
	}
}

func TestTruncate(t *testing.T) {
	r, v := vfsRig(t)
	p := r.spawn()
	path := r.str(p, scratch, "/tmp/t.txt")
	fd := uint64(r.call(p, 2, path, 0x41, 0o644)) // O_WRONLY|O_CREAT
	r.put(p, scratch+64, []byte("hello world"))
	r.call(p, 1, fd, scratch+64, 11)
	if r.call(p, 77, fd, 5) != 0 {
		t.Fatal("ftruncate")
	}
	if got, _ := fs.ReadFile(v, "tmp/t.txt"); string(got) != "hello" {
		t.Errorf("after ftruncate(5): %q", got)
	}
	if r.call(p, 76, path, 8) != 0 {
		t.Fatal("truncate")
	}
	if got, _ := fs.ReadFile(v, "tmp/t.txt"); string(got) != "hello\x00\x00\x00" {
		t.Errorf("after truncate(8): %q", got)
	}
	if e := r.call(p, 77, 1, 0); e != -int64(syscall.EINVAL) {
		t.Errorf("ftruncate of the console = %d, want EINVAL", e)
	}
	ro := uint64(r.call(p, 2, r.str(p, scratch+128, "/etc/motd"), 0))
	if e := r.call(p, 77, ro, 0); e != -int64(syscall.EBADF) {
		t.Errorf("ftruncate of a read-only fd = %d, want EBADF", e)
	}
}

func TestRenameFsyncChmod(t *testing.T) {
	r, v := vfsRig(t)
	p := r.spawn()
	old, nu := r.str(p, scratch, "/tmp/draft.c"), r.str(p, scratch+64, "/tmp/final.c")
	fd := uint64(r.call(p, 2, old, 0x41, 0o644))
	r.put(p, scratch+512, []byte("int x;"))
	r.call(p, 1, fd, scratch+512, 6)
	if r.call(p, 74, fd) != 0 || r.call(p, 75, fd) != 0 {
		t.Error("fsync/fdatasync of an open file")
	}
	if e := r.call(p, 74, 77); e != -int64(syscall.EBADF) {
		t.Errorf("fsync of a closed fd = %d", e)
	}
	if r.call(p, 82, old, nu) != 0 { // rename
		t.Fatal("rename")
	}
	if b, err := fs.ReadFile(v, "tmp/final.c"); err != nil || string(b) != "int x;" {
		t.Errorf("after rename: %q, %v", b, err)
	}
	if _, err := fs.Stat(v, "tmp/draft.c"); err == nil {
		t.Error("the old name is still there")
	}
	other := r.str(p, scratch+128, "/tmp/other.c")
	r.call(p, 2, other, 0x41, 0o644)
	cwd := uint64(0xFFFFFF9C)
	if e := r.call(p, 316, cwd, other, cwd, nu, 1); e != -int64(syscall.EEXIST) { // renameat2 NOREPLACE
		t.Errorf("RENAME_NOREPLACE onto an existing file = %d", e)
	}
	if e := r.call(p, 316, cwd, other, cwd, nu, 4); e != -int64(syscall.EINVAL) {
		t.Errorf("an unknown renameat2 flag = %d", e)
	}
	if e := r.call(p, 82, nu, r.str(p, scratch+192, "/etc/final.c")); e != -int64(syscall.EXDEV) {
		t.Errorf("rename across mounts = %d, want EXDEV", e)
	}
	if r.call(p, 90, nu, 0o755) != 0 || r.call(p, 268, cwd, nu, 0o600, 0) != 0 || r.call(p, 280, cwd, nu, 0, 0) != 0 {
		t.Error("chmod, fchmodat or utimensat of an existing file")
	}
	if e := r.call(p, 90, old, 0o755); e != -int64(syscall.ENOENT) {
		t.Errorf("chmod of a missing file = %d", e)
	}
}

// BUG-0016: FIONREAD on a pipe says how many bytes wait (QProcess reads
// that many; without it Qt read one byte of a child's output).
func TestBUG0016PipeFIONREAD(t *testing.T) {
	r, _ := vfsRig(t)
	p := r.spawn()
	r.call(p, 22, scratch) // 3 reads, 4 writes
	r.call(p, 1, 4, r.str(p, scratch+64, "hello pipe"), 10)
	if e := r.call(p, 16, 3, 0x541B, scratch+128); e != 0 || le.Uint32(read(p, scratch+128, 4)) != 10 {
		t.Errorf("FIONREAD = %d, %d bytes", e, le.Uint32(read(p, scratch+128, 4)))
	}
}
