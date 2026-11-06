package proc_test

import (
	"strconv"
	"strings"
	"syscall"
	"testing"

	"burrow/kernel/procfs"
)

// TestProcFDLinks: /proc/<pid>/fd as a process reads it, opens it, and
// another user is refused (W23-S03).
func TestProcFDLinks(t *testing.T) {
	r, v := vfsRig(t)
	v.Mount("proc", procfs.New(r.sys, procfs.Static{CPUs: 1}), "proc")
	p := r.spawn()
	if r.call(p, 293, scratch+1024, 0) != 0 { // pipe2: fds 3 and 4
		t.Fatal("pipe2")
	}
	pipe, _ := r.readlink(p, "/proc/self/fd/3")
	other, _ := r.readlink(p, "/proc/"+strconv.Itoa(p.PID)+"/fd/4")
	if !strings.HasPrefix(pipe, "pipe:[") || pipe != other {
		t.Fatalf("fd 3 -> %q, fd 4 -> %q", pipe, other)
	}
	w := r.call(p, 2, r.str(p, scratch, "/proc/self/fd/4"), 1) // open(O_WRONLY): the same pipe
	if w != 5 || r.call(p, 1, uint64(w), r.str(p, scratch, "hi"), 2) != 2 {
		t.Fatalf("open fd/4 = %d", w)
	}
	if n := r.call(p, 0, 3, scratch+256, 16); n != 2 || string(read(p, scratch+256, 2)) != "hi" {
		t.Fatalf("read the pipe = %d", n)
	}
	f := r.call(p, 2, r.str(p, scratch, "/etc/motd"), 0)
	if got, _ := r.readlink(p, "/proc/self/fd/"+strconv.Itoa(int(f))); got != "/etc/motd" {
		t.Fatalf("fd %d -> %q", f, got)
	}
	again := r.call(p, 2, r.str(p, scratch, "/proc/self/fd/"+strconv.Itoa(int(f))), 0) // reopened by its path
	if n := r.call(p, 0, uint64(again), scratch+256, 5); again < 0 || n != 5 || string(read(p, scratch+256, 5)) != "hello" {
		t.Fatalf("reopen = %d, read %d", again, n)
	}
	if exe, _ := r.readlink(p, "/proc/self/exe"); exe != "/bin/prog" {
		t.Errorf("exe -> %q", exe)
	}
	q := r.user(1000)
	path := "/proc/" + strconv.Itoa(p.PID) + "/fd/3"
	if _, e := r.readlink(q, path); e != -int64(syscall.EACCES) {
		t.Errorf("another user's readlink = %d", e)
	}
	if e := r.call(q, 2, r.str(q, scratch, path), 0); e != -int64(syscall.EACCES) {
		t.Errorf("another user's open = %d", e)
	}
	if e := r.call(q, 2, r.str(q, scratch, "/proc/self/fd/0"), 1); e < 0 {
		t.Errorf("its own console = %d", e)
	}
}
