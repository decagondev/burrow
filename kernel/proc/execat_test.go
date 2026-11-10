package proc_test

import (
	"syscall"
	"testing"
)

// TestExecveat (W24-S02): dropbear runs its own program again from the
// descriptor it holds (AT_EMPTY_PATH); a path relative to a directory
// descriptor works too, and a descriptor with no path cannot be run.
func TestExecveat(t *testing.T) {
	r := newRig(t)
	p := r.spawn()
	fd := uint64(r.call(p, 2, r.str(p, scratch, "/bin/prog"), 0))
	oldAS := r.nu.Thread(p.TID()).AS
	r.put(p, scratch, []byte{0})
	if e := r.call(p, 322, fd, scratch, 0, 0, 0x1000); e != 0 { // AT_EMPTY_PATH
		t.Fatalf("execveat(fd, \"\", AT_EMPTY_PATH) = %d", e)
	}
	if th := r.nu.Thread(p.TID()); th.AS == oldAS || th.RIP != 0x400200 {
		t.Errorf("after execveat: as %d rip %#x", th.AS, th.RIP)
	}
	dir := uint64(r.call(p, 2, r.str(p, scratch, "/bin"), 0o200000)) // O_DIRECTORY
	if e := r.call(p, 322, dir, r.str(p, scratch, "prog"), 0, 0, 0); e != 0 {
		t.Errorf("execveat(dir, \"prog\") = %d", e)
	}
	if r.call(p, 22, scratch+512) != 0 { // pipe
		t.Fatal("pipe")
	}
	r.put(p, scratch, []byte{0})
	if e := r.call(p, 322, uint64(le.Uint32(read(p, scratch+512, 4))), scratch, 0, 0, 0x1000); e != -int64(syscall.EACCES) {
		t.Errorf("execveat of a pipe = %d", e)
	}
	if e := r.call(p, 322, fd, scratch, 0, 0, 0x4); e != -int64(syscall.EINVAL) {
		t.Errorf("execveat with an unknown flag = %d", e)
	}
}
