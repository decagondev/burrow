package proc_test

import (
	"bytes"
	"syscall"
	"testing"

	"burrow/kernel/proc"
)

const (
	protR, protRW       = 1, 3
	mapSharedF, mapPriv = 0x01, 0x02
	mapFixedF           = 0x10
)

// bigFile is three whole pages of 'A', 'B', 'C' and 100 bytes of 'D'.
func bigFile() []byte {
	var b []byte
	for _, c := range "ABC" {
		b = append(b, bytes.Repeat([]byte{byte(c)}, 4096)...)
	}
	return append(b, bytes.Repeat([]byte{'D'}, 100)...)
}

// mapRig has /tmp/big and an open read-only descriptor for it.
func mapRig(t *testing.T) (*rig, *proc.Process, uint64) {
	r, v := vfsRig(t)
	if err := v.WriteFile("tmp/big", bigFile()); err != nil {
		t.Fatal(err)
	}
	p := r.spawn()
	fd := r.call(p, 2, r.str(p, scratch, "/tmp/big"), 0)
	if fd < 0 {
		t.Fatalf("open = %d", fd)
	}
	return r, p, uint64(fd)
}

func TestMmapFile(t *testing.T) {
	r, p, fd := mapRig(t)
	before := r.nu.FramesInUse()
	va := uint64(r.call(p, 9, 0, 5*4096, protR, mapPriv, fd, 0))
	if int64(va) < 0 || r.nu.FramesInUse() != before {
		t.Fatalf("mmap = %d, %d frames at once", int64(va), r.nu.FramesInUse()-before)
	}
	r.call(p, 3, fd) // the mapping outlives the descriptor
	if got := read(p, va+4096, 3); string(got) != "BBB" {
		t.Errorf("page 1 = %q", got)
	}
	tail := read(p, va+3*4096, 4096) // 100 bytes of D, then zeros
	if !bytes.Equal(tail[:100], bytes.Repeat([]byte{'D'}, 100)) || !bytes.Equal(tail[100:], make([]byte, 3996)) {
		t.Error("the last page: want the file's 100 bytes, then zeros")
	}
	if got := read(p, va+4*4096, 8); !bytes.Equal(got, make([]byte, 8)) {
		t.Errorf("past the end of the file: %q", got)
	}
	if p.Mem().FaultIn(va, true) != proc.FaultSignal {
		t.Error("a write to a read-only file mapping did not fault")
	}
	if p.Mem().FaultIn(va+2*4096, false) != proc.FaultResume || read(p, va+2*4096, 1)[0] != 'C' {
		t.Error("a read fault did not fill page 2 from the file (one page at a time: no fault-around for mmap)")
	}
}

func TestMmapFileOffsetAndSplits(t *testing.T) {
	r, p, fd := mapRig(t)
	va := uint64(r.call(p, 9, 0, 3*4096, protR, mapPriv, fd, 4096)) // from page 1: B, C, D
	if read(p, va, 1)[0] != 'B' {
		t.Errorf("offset 4096 maps %q", read(p, va, 1))
	}
	r.call(p, 11, va+4096, 4096) // unmap the middle (C)
	r.call(p, 10, va, 4096, 0x3) // mprotect the first: the areas split
	if got := read(p, va+2*4096, 1)[0]; got != 'D' {
		t.Errorf("after the splits the third page reads %q, want D", got)
	}
	if e := r.call(p, 9, 0, 4096, protR, mapPriv, fd, 100); e != -int64(syscall.EINVAL) {
		t.Errorf("an unaligned offset = %d", e)
	}
	fixed := uint64(0x2000_0000_0000)
	if got := uint64(r.call(p, 9, fixed, 4096, protR, mapPriv|mapFixedF, fd, 8192)); got != fixed || read(p, fixed, 1)[0] != 'C' {
		t.Errorf("MAP_FIXED file mapping at %#x", got)
	}
}

// Writes to a private mapping stay in the process (and its fork's copy);
// MADV_DONTNEED drops them and the file shows through again.
func TestMmapFilePrivateWrites(t *testing.T) {
	r, p, fd := mapRig(t)
	va := uint64(r.call(p, 9, 0, 4096, protRW, mapPriv, fd, 0))
	r.put(p, va, []byte("mine"))
	kid, err := p.Mem().Clone() // what fork does to the memory
	if err != nil {
		t.Fatal(err)
	}
	defer kid.Close()
	got := make([]byte, 5)
	kid.CopyIn(va, got)
	if string(got) != "mineA" {
		t.Errorf("the fork's copy = %q", got)
	}
	r.call(p, 8, fd, 0, 0) // lseek 0, then read the file itself
	if r.call(p, 0, fd, scratch+512, 4); string(read(p, scratch+512, 4)) != "AAAA" {
		t.Error("a private write reached the file")
	}
	r.call(p, 28, va, 4096, 4) // MADV_DONTNEED
	if string(read(p, va, 4)) != "AAAA" {
		t.Errorf("after MADV_DONTNEED: %q", read(p, va, 4))
	}
}

func TestMmapFileRefusals(t *testing.T) {
	r, p, fd := mapRig(t)
	if va := r.call(p, 9, 0, 4096, protR, mapSharedF, fd, 0); va < 0 || read(p, uint64(va), 1)[0] != 'A' {
		t.Errorf("read-only MAP_SHARED = %d", va)
	}
	if e := r.call(p, 9, 0, 4096, protRW, mapSharedF, fd, 0); e != -int64(syscall.ENODEV) {
		t.Errorf("writable MAP_SHARED = %d, want ENODEV", e)
	}
	if e := r.call(p, 9, 0, 4096, protR, 0, fd, 0); e != -int64(syscall.EINVAL) {
		t.Errorf("neither shared nor private = %d", e)
	}
	wr := uint64(r.call(p, 2, r.str(p, scratch, "/tmp/big"), 1)) // O_WRONLY
	if e := r.call(p, 9, 0, 4096, protR, mapPriv, wr, 0); e != -int64(syscall.EACCES) {
		t.Errorf("a write-only descriptor = %d, want EACCES", e)
	}
	r.call(p, 22, scratch) // a pipe
	pipeFD := uint64(le.Uint32(read(p, scratch, 4)))
	if e := r.call(p, 9, 0, 4096, protR, mapPriv, pipeFD, 0); e != -int64(syscall.ENODEV) {
		t.Errorf("a pipe = %d, want ENODEV", e)
	}
	dir := uint64(r.call(p, 2, r.str(p, scratch, "/etc"), 0x10000)) // O_DIRECTORY
	if e := r.call(p, 9, 0, 4096, protR, mapPriv, dir, 0); e != -int64(syscall.ENODEV) {
		t.Errorf("a directory = %d, want ENODEV", e)
	}
}
