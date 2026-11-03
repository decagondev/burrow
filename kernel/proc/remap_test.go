package proc_test

import (
	"syscall"
	"testing"
)

const mapPrivAnon = 0x22 // MAP_PRIVATE | MAP_ANONYMOUS

// mremap as musl's realloc uses it: shrink and grow in place, and move with
// MREMAP_MAYMOVE when the range above is taken, keeping the data.
func TestMremap(t *testing.T) {
	r := newRig(t)
	p := r.spawn()
	a := uint64(r.call(p, 9, 0, 4*4096, 3, mapPrivAnon, ^uint64(0), 0))
	r.put(p, a, []byte("keep me"))
	r.put(p, a+3*4096, []byte("page 3"))
	if got := r.call(p, 25, a, 4*4096, 2*4096, 0, 0); uint64(got) != a || p.Mem().Mapped(a+2*4096, 1) {
		t.Fatalf("shrink = %#x, the tail still mapped %v", got, p.Mem().Mapped(a+2*4096, 1))
	}
	if got := r.call(p, 25, a, 2*4096, 3*4096, 0, 0); uint64(got) != a || !p.Mem().Mapped(a+2*4096, 1) {
		t.Fatalf("grow in place = %#x", got)
	}
	b := uint64(r.call(p, 9, a+3*4096, 4096, 3, mapPrivAnon|0x10, ^uint64(0), 0)) // MAP_FIXED just above
	if b != a+3*4096 {
		t.Fatalf("map above = %#x", b)
	}
	if e := r.call(p, 25, a, 3*4096, 5*4096, 0, 0); e != -int64(syscall.ENOMEM) {
		t.Errorf("grow into a taken range without MAYMOVE = %d, want ENOMEM", e)
	}
	moved := uint64(r.call(p, 25, a, 3*4096, 8*4096, 1, 0)) // MREMAP_MAYMOVE
	if moved == a || int64(moved) < 0 {
		t.Fatalf("move = %#x", moved)
	}
	if string(read(p, moved, 7)) != "keep me" || p.Mem().Mapped(a, 1) || !p.Mem().Mapped(moved+7*4096, 1) {
		t.Errorf("after the move: %q, old mapped %v", read(p, moved, 7), p.Mem().Mapped(a, 1))
	}
	if e := r.call(p, 25, a, 4096, 8192, 1, 0); e != -int64(syscall.EFAULT) {
		t.Errorf("mremap of nothing = %d, want EFAULT", e)
	}
	if e := r.call(p, 25, moved, 4096, 4096, 2, 0); e != -int64(syscall.EINVAL) {
		t.Errorf("MREMAP_FIXED without MAYMOVE = %d", e)
	}
}

func TestStatfsAndFadvise(t *testing.T) {
	r, _ := vfsRig(t)
	p := r.spawn()
	if e := r.call(p, 137, r.str(p, scratch, "/tmp"), scratch+256); e != 0 || le.Uint64(read(p, scratch+256, 8)) != 0x01021994 {
		t.Errorf("statfs /tmp = %d, type %#x, want TMPFS_MAGIC", e, le.Uint64(read(p, scratch+256, 8)))
	}
	if e := r.call(p, 137, r.str(p, scratch, "/nope"), scratch+256); e != -int64(syscall.ENOENT) {
		t.Errorf("statfs of nothing = %d", e)
	}
	fd := uint64(r.call(p, 2, r.str(p, scratch, "/etc/motd"), 0))
	if e := r.call(p, 138, fd, scratch+256); e != 0 || le.Uint64(read(p, scratch+256+64, 8)) != 255 {
		t.Errorf("fstatfs = %d, namelen %d", e, le.Uint64(read(p, scratch+256+64, 8)))
	}
	if e := r.call(p, 221, fd, 0, 0, 2); e != 0 { // POSIX_FADV_SEQUENTIAL
		t.Errorf("fadvise64 = %d", e)
	}
	if e := r.call(p, 86, r.str(p, scratch, "/etc/motd"), r.str(p, scratch+128, "/tmp/m")); e != -int64(syscall.EPERM) {
		t.Errorf("link = %d, want EPERM", e)
	}
	if e := r.call(p, 86, r.str(p, scratch, "/nope"), r.str(p, scratch+128, "/tmp/m")); e != -int64(syscall.ENOENT) {
		t.Errorf("link of nothing = %d, want ENOENT", e)
	}
	r.call(p, 22, scratch)
	if e := r.call(p, 221, uint64(le.Uint32(read(p, scratch, 4))), 0, 0, 0); e != -int64(syscall.ESPIPE) {
		t.Errorf("fadvise64 on a pipe = %d, want ESPIPE", e)
	}
}

// KDGKBENT on the terminal: TinyX builds its keymap from it.
func TestKDGKBENT(t *testing.T) {
	r, _, _ := vtRig(t)
	p := r.spawn()
	r.put(p, scratch, []byte{1, 30, 0, 0}) // the Shift table, key code 30 (A)
	if e := r.call(p, 16, 0, 0x4B46, scratch); e != 0 || le.Uint16(read(p, scratch+2, 2)) != 0x0b41 {
		t.Errorf("KDGKBENT shift+a = %d, %#x, want K(KT_LETTER,'A')", e, le.Uint16(read(p, scratch+2, 2)))
	}
	r.put(p, scratch, []byte{0, 200, 0, 0})
	if e := r.call(p, 16, 0, 0x4B46, scratch); e != -int64(syscall.EINVAL) {
		t.Errorf("KDGKBENT key 200 = %d", e)
	}
}

// setitimer, getitimer and alarm (W16-S02: TinyX's scheduler).
func TestItimer(t *testing.T) {
	r := newRig(t)
	p := r.spawn()
	val := make([]byte, 32) // it_interval 0, it_value 10 s
	le.PutUint64(val[16:], 10)
	r.put(p, scratch, val)
	if e := r.call(p, 38, 0, scratch, 0); e != 0 {
		t.Fatalf("setitimer = %d", e)
	}
	if e := r.call(p, 36, 0, scratch+64); e != 0 || le.Uint64(read(p, scratch+64+16, 8)) != 9 {
		t.Errorf("getitimer = %d, %d s left, want 9 and some", e, le.Uint64(read(p, scratch+64+16, 8)))
	}
	if left := r.call(p, 37, 0); left != 10 {
		t.Errorf("alarm(0) = %d, want 10 (rounded)", left)
	}
	if r.call(p, 36, 0, scratch+64); le.Uint64(read(p, scratch+64+16, 8))|le.Uint64(read(p, scratch+64+24, 8)) != 0 {
		t.Error("the timer is still armed after alarm(0)")
	}
	if e := r.call(p, 38, 3, scratch, 0); e != -int64(syscall.EINVAL) {
		t.Errorf("setitimer(3) = %d, want EINVAL", e)
	}
}
