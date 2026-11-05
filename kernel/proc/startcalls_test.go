package proc_test

import (
	"bytes"
	"syscall"
	"testing"

	"burrow/kernel/proc"
)

func read(p *proc.Process, va uint64, n int) []byte {
	b := make([]byte, n)
	p.Mem().CopyIn(va, b)
	return b
}

func TestGetrandom(t *testing.T) {
	r := newRig(t)
	p := r.spawn()
	if n := r.call(p, 318, scratch, 64, 0); n != 64 {
		t.Fatalf("getrandom = %d", n)
	}
	a := read(p, scratch, 64)
	r.call(p, 318, scratch, 64, 1) // GRND_NONBLOCK
	if bytes.Equal(a, read(p, scratch, 64)) || bytes.Equal(a, make([]byte, 64)) {
		t.Error("two getrandom calls gave the same bytes")
	}
	if e := r.call(p, 318, scratch, 8, 8); e != -int64(syscall.EINVAL) {
		t.Errorf("unknown flag = %d", e)
	}
	if e := r.call(p, 318, scratch, 8, 6); e != -int64(syscall.EINVAL) { // GRND_RANDOM|GRND_INSECURE
		t.Errorf("GRND_RANDOM|GRND_INSECURE = %d", e)
	}
}

func TestStatx(t *testing.T) {
	r := newRig(t)
	p := r.spawn()
	r.str(p, scratch, "/etc/motd")
	if e := r.call(p, 332, atFDCWD, scratch, 0, 0x7ff, scratch+256); e != 0 {
		t.Fatalf("statx = %d", e)
	}
	x := read(p, scratch+256, 256)
	r.call(p, 4, scratch, scratch+512) // stat
	st := read(p, scratch+512, 144)
	if le.Uint32(x) != 0x7ff || le.Uint64(x[40:]) != 11 || le.Uint16(x[28:]) != uint16(le.Uint32(st[24:])) ||
		le.Uint64(x[32:]) != le.Uint64(st[8:]) || le.Uint64(x[112:]) != le.Uint64(st[88:]) {
		t.Errorf("statx mask %#x size %d mode %#o: does not match stat", le.Uint32(x), le.Uint64(x[40:]), le.Uint16(x[28:]))
	}
	if e := r.call(p, 332, atFDCWD, r.str(p, scratch, "/nope"), 0, 0x7ff, scratch+256); e != -int64(syscall.ENOENT) {
		t.Errorf("statx of a missing file = %d", e)
	}
	r.str(p, scratch, "")
	if e := r.call(p, 332, 1, scratch, 0x1000, 0x7ff, scratch+256); e != 0 { // AT_EMPTY_PATH on fd 1
		t.Errorf("statx AT_EMPTY_PATH = %d", e)
	}
	if e := r.call(p, 332, atFDCWD, scratch, 0x1, 0x7ff, scratch+256); e != -int64(syscall.EINVAL) {
		t.Errorf("statx with a bad flag = %d", e)
	}
}

const atFDCWD = uint64(0xFFFFFF9C) // AT_FDCWD as the int a C caller passes

func TestReadlinkUmaskRusage(t *testing.T) {
	r := newRig(t)
	p := r.spawn()
	r.str(p, scratch, "/etc/motd")
	if e := r.call(p, 89, scratch, scratch+256, 64); e != -int64(syscall.EINVAL) {
		t.Errorf("readlink of a file = %d, want EINVAL", e)
	}
	if e := r.call(p, 267, atFDCWD, r.str(p, scratch, "/nope"), scratch+256, 64); e != -int64(syscall.ENOENT) {
		t.Errorf("readlinkat of nothing = %d, want ENOENT", e)
	}
	if old := r.call(p, 95, 0o077); old != 0o022 {
		t.Errorf("first umask = %#o, want 022", old)
	}
	r.call(p, 57) // fork: the child inherits the mask
	kid := r.newestThread(p.TID())
	if old := r.callAs(kid, 95, 0); old != 0o077 {
		t.Errorf("child's umask = %#o, want 077", old)
	}
	r.put(p, scratch, bytes.Repeat([]byte{0xAA}, 144))
	if e := r.call(p, 98, 0, scratch); e != 0 || !bytes.Equal(read(p, scratch, 144), make([]byte, 144)) {
		t.Errorf("getrusage = %d, want zeros", e)
	}
	if e := r.call(p, 98, 5, scratch); e != -int64(syscall.EINVAL) {
		t.Errorf("getrusage(5) = %d", e)
	}
}

func TestSysinfo(t *testing.T) {
	r := newRig(t)
	p := r.spawn()
	if e := r.call(p, 99, scratch); e != 0 {
		t.Fatalf("sysinfo = %d", e)
	}
	b := read(p, scratch, 112)
	total, free, procs, unit := le.Uint64(b[32:]), le.Uint64(b[40:]), le.Uint16(b[80:]), le.Uint32(b[104:])
	if total != 1<<30 || free == 0 || free >= total || procs != 2 || unit != 1 { // init and p
		t.Errorf("sysinfo total %d free %d procs %d unit %d", total, free, procs, unit)
	}
}

func TestSchedAndMembarrier(t *testing.T) {
	r := newRig(t)
	p := r.spawn()
	if e := r.call(p, 143, 0, scratch); e != 0 || le.Uint32(read(p, scratch, 4)) != 0 {
		t.Errorf("sched_getparam = %d", e)
	}
	if e := r.call(p, 145, uint64(p.PID)); e != 0 {
		t.Errorf("sched_getscheduler = %d, want SCHED_OTHER", e)
	}
	if e := r.call(p, 145, 999); e != -int64(syscall.ESRCH) {
		t.Errorf("sched_getscheduler(999) = %d, want ESRCH", e)
	}
	restore := proc.SetNumCPU(1)
	defer restore()
	r.put(p, scratch, []byte{1, 0, 0, 0, 0, 0, 0, 0})
	if e := r.call(p, 203, 0, 8, scratch); e != 0 {
		t.Errorf("sched_setaffinity(cpu 0) = %d", e)
	}
	r.put(p, scratch, []byte{2})
	if e := r.call(p, 203, 0, 8, scratch); e != -int64(syscall.EINVAL) {
		t.Errorf("sched_setaffinity(cpu 1 only) on one CPU = %d, want EINVAL", e)
	}
	proc.SetNumCPU(4) // W20-S08; the deferred restore still puts the real count back
	if e := r.call(p, 203, 0, 8, scratch); e != 0 {
		t.Errorf("sched_setaffinity(cpu 1 only) on four CPUs = %d", e)
	}
	r.put(p, scratch, []byte{0x10})
	if e := r.call(p, 203, 0, 8, scratch); e != -int64(syscall.EINVAL) {
		t.Errorf("sched_setaffinity(cpu 4 only) on four CPUs = %d, want EINVAL", e)
	}
	if e := r.call(p, 204, 0, 8, scratch); e != 8 || le.Uint64(read(p, scratch, 8)) != 0xF {
		t.Errorf("sched_getaffinity on four CPUs = %d, mask %#x", e, le.Uint64(read(p, scratch, 8)))
	}
	if n := r.call(p, 324, 0, 0); n != 0 {
		t.Errorf("membarrier QUERY = %d, want 0 (no commands)", n)
	}
	if e := r.call(p, 324, 1, 0); e != -int64(syscall.EINVAL) {
		t.Errorf("membarrier GLOBAL = %d, want EINVAL", e)
	}
}
