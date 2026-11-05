package proc_test

// perm_test.go - owners and permissions (W19-S04) through the system calls
// on a real VFS (memfs at /tmp): new files' owners, the umask, a user
// denied, the sticky bit, chmod and chown rules, access with the real ids,
// execute permission, and a set-uid program in /tmp. permtest checks the
// same on the host's Linux and on Burrow.

import (
	"syscall"
	"testing"

	"burrow/kernel/proc"
)

const (
	nrOpen, nrClose, nrStat, nrAccess, nrUnlink, nrChmod, nrChown = 2, 3, 4, 21, 87, 90, 92
	oWRONLY, oCREAT                                               = 0o1, 0o100
)

// user is a process of uid (and gid) id; root sets /tmp to 1777 first.
func permRig(t *testing.T) (*rig, *proc.Process) {
	r, _ := vfsRig(t)
	root := r.spawn()
	if e := r.call(root, nrChmod, r.str(root, scratch, "/tmp"), 0o1777); e != 0 {
		t.Fatalf("chmod /tmp = %d", e)
	}
	return r, root
}

func (r *rig) user(id uint64) *proc.Process {
	p := r.spawn()
	r.call(p, nrSetresgid, id, id, id)
	r.call(p, nrSetresuid, id, id, id)
	return p
}

// create makes path with mode (as p) and returns the open's answer.
func (r *rig) create(p *proc.Process, path string, mode uint64) int64 {
	fd := r.call(p, nrOpen, r.str(p, scratch, path), oWRONLY|oCREAT, mode)
	if fd >= 0 {
		r.call(p, nrClose, uint64(fd))
	}
	return fd
}

func (r *rig) statOf(p *proc.Process, path string) (mode, uid, gid uint32) {
	if e := r.call(p, nrStat, r.str(p, scratch, path), scratch+256); e != 0 {
		r.t.Fatalf("stat %s = %d", path, e)
	}
	st := make([]byte, 144)
	p.Mem().CopyIn(scratch+256, st)
	return le.Uint32(st[24:]) & 0o7777, le.Uint32(st[28:]), le.Uint32(st[32:])
}

func TestOwnersOfNewFiles(t *testing.T) {
	r, root := permRig(t)
	u := r.user(1000)
	r.call(u, 95, 0o022) // umask
	if fd := r.create(u, "/tmp/mine", 0o666); fd < 0 {
		t.Fatalf("a user creating in /tmp: %d", fd)
	}
	if m, uid, gid := r.statOf(u, "/tmp/mine"); m != 0o644 || uid != 1000 || gid != 1000 {
		t.Errorf("/tmp/mine: mode %o, %d:%d", m, uid, gid)
	}
	if e := r.call(u, 83, r.str(u, scratch, "/tmp/d"), 0o777); e != 0 { // mkdir
		t.Fatalf("mkdir = %d", e)
	}
	if m, uid, _ := r.statOf(u, "/tmp/d"); m != 0o755 || uid != 1000 {
		t.Errorf("/tmp/d: mode %o owner %d", m, uid)
	}
	if fd := r.create(u, "/etc/x", 0o644); fd != -int64(syscall.EACCES) {
		t.Errorf("a user creating in root's /etc: %d", fd)
	}
	r.call(root, nrChmod, r.str(root, scratch, "/tmp/mine"), 0o600)
	if m, _, _ := r.statOf(root, "/tmp/mine"); m != 0o600 {
		t.Errorf("root's chmod of a user's file: %o", m)
	}
}

func TestUserDenied(t *testing.T) {
	r, root := permRig(t)
	r.create(root, "/tmp/secret", 0o600)
	r.call(root, nrChmod, r.str(root, scratch, "/tmp/secret"), 0o600)
	u := r.user(1000)
	eacces, eperm := -int64(syscall.EACCES), -int64(syscall.EPERM)
	if fd := r.call(u, nrOpen, r.str(u, scratch, "/tmp/secret"), 0, 0); fd != eacces {
		t.Errorf("a user reading root's 0600 file: %d", fd)
	}
	if e := r.call(u, nrUnlink, r.str(u, scratch, "/tmp/secret")); e != eperm {
		t.Errorf("a user unlinking root's file in sticky /tmp: %d", e)
	}
	if e := r.call(u, nrChmod, r.str(u, scratch, "/tmp/secret"), 0o666); e != eperm {
		t.Errorf("a user's chmod of root's file: %d", e)
	}
	r.create(u, "/tmp/mine", 0o644)
	if e := r.call(u, nrChown, r.str(u, scratch, "/tmp/mine"), 0, noID); e != eperm {
		t.Errorf("a user giving a file away: %d", e)
	}
	if e := r.call(u, nrChown, r.str(u, scratch, "/tmp/mine"), noID, 50); e != eperm {
		t.Errorf("a user's chgrp to a group not held: %d", e)
	}
	if fd := r.call(root, nrOpen, r.str(root, scratch, "/tmp/secret"), 0, 0); fd < 0 {
		t.Errorf("root reading a 0600 file: %d", fd)
	}
}

// TestAccessUsesRealIDs: real uid 1000, effective root.
func TestAccessUsesRealIDs(t *testing.T) {
	r, root := permRig(t)
	r.create(root, "/tmp/secret", 0o600)
	r.call(root, nrChmod, r.str(root, scratch, "/tmp/secret"), 0o600)
	r.call(root, nrSetresuid, 1000, 0, 0)
	if e := r.call(root, nrAccess, r.str(root, scratch, "/tmp/secret"), 4); e != -int64(syscall.EACCES) {
		t.Errorf("access(R_OK) with real uid 1000: %d", e)
	}
	if e := r.call(root, nrAccess, r.str(root, scratch, "/tmp/secret"), 0); e != 0 {
		t.Errorf("access(F_OK): %d", e)
	}
	if fd := r.call(root, nrOpen, r.str(root, scratch, "/tmp/secret"), 0, 0); fd < 0 {
		t.Errorf("open with effective root: %d", fd)
	}
}

// TestExecuteAndSetuidInTmp: a program root copies to /tmp runs only with
// an execute bit for the caller; set-uid there makes its owner effective.
func TestExecuteAndSetuidInTmp(t *testing.T) {
	r, root := permRig(t)
	prog := program()
	fd := r.call(root, nrOpen, r.str(root, scratch, "/tmp/p"), oWRONLY|oCREAT, 0o700)
	r.put(root, scratch+0x1000, prog)
	if n := r.call(root, 1, uint64(fd), scratch+0x1000, uint64(len(prog))); n != int64(len(prog)) {
		t.Fatalf("write = %d", n)
	}
	r.call(root, nrClose, uint64(fd))
	u := r.user(2000)
	if e := r.call(u, 59, r.str(u, scratch, "/tmp/p"), 0, 0); e != -int64(syscall.EACCES) {
		t.Errorf("a user running root's 0700 program: %d", e)
	}
	r.call(root, nrChown, r.str(root, scratch, "/tmp/p"), 1000, 1000)
	r.call(root, nrChmod, r.str(root, scratch, "/tmp/p"), 0o4755)
	if m, _, _ := r.statOf(root, "/tmp/p"); m != 0o4755 {
		t.Fatalf("mode after chmod 4755: %o", m)
	}
	if e := r.call(u, 59, r.str(u, scratch, "/tmp/p"), 0, 0); e != 0 {
		t.Fatalf("running the set-uid program: %d", e)
	}
	if got := r.res(u, nrGetresuid); got != [3]uint32{2000, 1000, 1000} {
		t.Errorf("after a set-uid program in /tmp: uids %v", got)
	}
	r.call(root, nrChown, r.str(root, scratch, "/tmp/p"), 1001, noID)
	if m, uid, _ := r.statOf(root, "/tmp/p"); m != 0o755 || uid != 1001 {
		t.Errorf("chown should drop the set-uid bit: mode %o owner %d", m, uid)
	}
}

// TestBUG0025FstatOfAFileOpenForWriting: fstat on a file open for writing
// reports the file's owner and mode (busybox's passwd copies the old
// /etc/shadow's mode onto the new one from it), and the bytes written.
func TestBUG0025FstatOfAFileOpenForWriting(t *testing.T) {
	r, root := permRig(t)
	r.create(root, "/tmp/shadow", 0o600)
	r.call(root, nrChmod, r.str(root, scratch, "/tmp/shadow"), 0o600)
	r.call(root, nrChown, r.str(root, scratch, "/tmp/shadow"), 0, 42)
	fd := r.call(root, nrOpen, r.str(root, scratch, "/tmp/shadow"), 2, 0) // O_RDWR
	r.put(root, scratch+0x1000, []byte("root:x"))
	r.call(root, 1, uint64(fd), scratch+0x1000, 6)
	if e := r.call(root, 5, uint64(fd), scratch+256); e != 0 { // fstat
		t.Fatalf("fstat = %d", e)
	}
	st := read(root, scratch+256, 144)
	if mode, gid, size := le.Uint32(st[24:])&0o7777, le.Uint32(st[32:]), le.Uint64(st[48:]); mode != 0o600 || gid != 42 || size != 6 {
		t.Errorf("fstat of a file open for writing: mode %o gid %d size %d (want 600, 42, 6)", mode, gid, size)
	}
}
