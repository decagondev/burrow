package proc_test

// link_test.go - links through the system calls (W19-S05), on a real VFS:
// symlink, readlink, lstat against stat, O_NOFOLLOW, ".." after a link,
// chdir and getcwd through a link, unlink and rename of a link, hard links,
// and search permission on a directory a link leads through. linktest
// checks the same on the host's Linux and on Burrow.

import (
	"syscall"
	"testing"

	"burrow/kernel/proc"
)

const (
	nrLstat, nrLink, nrSymlink, nrReadlink, nrChdir, nrGetcwd, nrMkdir, nrRename = 6, 86, 88, 89, 80, 79, 83, 82
	oNOFOLLOW                                                                    = 0o400000
)

func (r *rig) readlink(p *proc.Process, path string) (string, int64) {
	n := r.call(p, nrReadlink, r.str(p, scratch, path), scratch+512, 64)
	if n < 0 {
		return "", n
	}
	return string(read(p, scratch+512, int(n))), n
}

func (r *rig) mode(p *proc.Process, nr uint64, path string) uint32 {
	if e := r.call(p, nr, r.str(p, scratch, path), scratch+256); e != 0 {
		return 0
	}
	return le.Uint32(read(p, scratch+256+24, 4)) &^ 0o7777
}

func TestLinkCalls(t *testing.T) {
	r, root := permRig(t)
	p := root
	r.call(p, nrMkdir, r.str(p, scratch, "/tmp/a"), 0o755)
	r.call(p, nrMkdir, r.str(p, scratch, "/tmp/a/b"), 0o755)
	r.create(p, "/tmp/a/x", 0o644)
	if e := r.call(p, nrSymlink, r.str(p, scratch, "a/b"), r.str(p, scratch+256, "/tmp/ab")); e != 0 {
		t.Fatalf("symlink = %d", e)
	}
	if target, n := r.readlink(p, "/tmp/ab"); target != "a/b" || n != 3 {
		t.Errorf("readlink = %q, %d", target, n)
	}
	if _, n := r.readlink(p, "/tmp/a/x"); n != -int64(syscall.EINVAL) {
		t.Errorf("readlink of a file = %d", n)
	}
	if m := r.mode(p, nrLstat, "/tmp/ab"); m != syscall.S_IFLNK {
		t.Errorf("lstat mode %o", m)
	}
	if m := r.mode(p, nrStat, "/tmp/ab"); m != syscall.S_IFDIR {
		t.Errorf("stat mode %o", m)
	}
	if fd := r.call(p, nrOpen, r.str(p, scratch, "/tmp/ab/../x"), 0, 0); fd < 0 { // the text says /tmp/x
		t.Errorf("open of .. after a link = %d", fd)
	}
	if fd := r.call(p, nrOpen, r.str(p, scratch, "/tmp/ab"), oNOFOLLOW, 0); fd != -int64(syscall.ELOOP) {
		t.Errorf("O_NOFOLLOW on a link = %d", fd)
	}
	if e := r.call(p, nrChdir, r.str(p, scratch, "/tmp/ab")); e != 0 {
		t.Fatalf("chdir through a link = %d", e)
	}
	if n := r.call(p, nrGetcwd, scratch+512, 64); n < 0 || string(read(p, scratch+512, int(n)-1)) != "/tmp/a/b" {
		t.Errorf("getcwd after chdir through a link = %d %q", n, read(p, scratch+512, 16))
	}
	if e := r.call(p, nrLink, r.str(p, scratch, "/tmp/a/x"), r.str(p, scratch+256, "../h")); e != 0 { // relative to /tmp/a/b
		t.Errorf("link = %d", e)
	}
	if e := r.call(p, nrRename, r.str(p, scratch, "/tmp/ab"), r.str(p, scratch+256, "/tmp/ab2")); e != 0 {
		t.Errorf("rename of a link = %d", e)
	}
	if target, _ := r.readlink(p, "/tmp/ab2"); target != "a/b" {
		t.Errorf("the renamed link points at %q", target)
	}
	if e := r.call(p, nrUnlink, r.str(p, scratch, "/tmp/ab2")); e != 0 || r.mode(p, nrStat, "/tmp/a/b") != syscall.S_IFDIR {
		t.Errorf("unlink of a link = %d (its target must stay)", e)
	}
	if e := r.call(p, nrLink, r.str(p, scratch, "/tmp/a"), r.str(p, scratch+256, "/tmp/a2")); e != -int64(syscall.EPERM) {
		t.Errorf("link of a directory = %d", e)
	}
}

// TestLinkSearchPermission: a link into a directory the user may not
// search does not open a way in.
func TestLinkSearchPermission(t *testing.T) {
	r, root := permRig(t)
	r.call(root, nrMkdir, r.str(root, scratch, "/tmp/closed"), 0o700)
	r.create(root, "/tmp/closed/f", 0o644)
	r.call(root, nrChmod, r.str(root, scratch, "/tmp/closed/f"), 0o644)
	r.call(root, nrSymlink, r.str(root, scratch, "closed/f"), r.str(root, scratch+256, "/tmp/way-in"))
	u := r.user(1000)
	if fd := r.call(u, nrOpen, r.str(u, scratch, "/tmp/way-in"), 0, 0); fd != -int64(syscall.EACCES) {
		t.Errorf("a user through a link into a closed directory = %d", fd)
	}
	if _, n := r.readlink(u, "/tmp/way-in"); n != 8 {
		t.Errorf("reading the link itself = %d", n)
	}
}
