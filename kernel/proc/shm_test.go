package proc_test

import (
	"syscall"
	"testing"
)

// W18-S03: MAP_SHARED|MAP_ANONYMOUS memory is the same frames after fork:
// what the child writes (the kernel writing it for the child: read(2) is
// CopyOut) the parent reads, unlike private memory, which S01 copies.
func TestSharedAnonymousAcrossFork(t *testing.T) {
	r := newRig(t)
	p := r.spawn()
	shared := uint64(r.call(p, 9, 0, 4096, 3, 0x21, ^uint64(0), 0)) // MAP_SHARED|MAP_ANONYMOUS
	if int64(shared) < 0 {
		t.Fatalf("mmap = %d", int64(shared))
	}
	r.str(p, shared, "parent")
	r.call(p, 22, scratch) // pipe: 3 and 4, before the fork so the child has them
	if r.call(p, 57) <= 0 {
		t.Fatal("fork failed")
	}
	kid := r.newestThread(p.TID())
	r.str(p, scratch+64, "child!")
	r.call(p, 1, 4, scratch+64, 6)                   // the parent writes "child!" into the pipe...
	if n := r.callAs(kid, 0, 3, shared, 6); n != 6 { // ...the child reads it into the shared page
		t.Fatalf("child's read = %d", n)
	}
	if got := string(read(p, shared, 6)); got != "child!" {
		t.Errorf("the parent sees %q in shared memory, want the child's write", got)
	}
}

// A memfd's frames are freed once nothing holds it: no descriptor, no
// mapped page. Until then the bytes stay, after close and after munmap of
// one of two mappings.
func TestMemfdLifetime(t *testing.T) {
	r := newRig(t)
	p := r.spawn()
	base := r.nu.FramesInUse()
	fd := uint64(r.call(p, 319, r.str(p, scratch, "lifetime"), 0))
	if r.call(p, 77, fd, 8192) != 0 { // ftruncate
		t.Fatal("ftruncate")
	}
	a := uint64(r.call(p, 9, 0, 8192, 3, 0x01, fd, 0)) // MAP_SHARED
	b := uint64(r.call(p, 9, 0, 8192, 3, 0x01, fd, 0))
	r.str(p, a+4096, "kept")
	r.call(p, 3, fd) // close: the mappings hold it
	if got := string(read(p, b+4096, 4)); got != "kept" {
		t.Fatalf("the second mapping reads %q", got)
	}
	r.call(p, 11, a, 8192) // munmap one
	if got := string(read(p, b+4096, 4)); got != "kept" {
		t.Fatalf("after one munmap the other reads %q", got)
	}
	r.call(p, 11, b, 8192)
	if n := r.nu.FramesInUse(); n != base {
		t.Errorf("%d frames left after the last mapping went", n-base)
	}
	if e := r.call(p, 319, r.str(p, scratch, "x"), 0x80); e != -int64(syscall.EINVAL) {
		t.Errorf("memfd_create with an unknown flag = %d", e)
	}
}
