package proc_test

// cred_test.go - users and groups (W19-S03) through the system calls:
// Linux's rules for the set*id family (credtest checks the same on the
// host's Linux and on Burrow), fork keeping the ids, a set-id execve with
// its auxiliary vector, and kill's permission check.

import (
	"io/fs"
	"syscall"
	"testing"
	"testing/fstest"

	"burrow/kernel/proc"
	"burrow/kernel/vfs"
)

const (
	nrGetuid, nrGetgid, nrSetuid, nrSetgid             = 102, 104, 105, 106
	nrGeteuid, nrGetegid, nrSetreuid                   = 107, 108, 113
	nrGetgroups, nrSetgroups                           = 115, 116
	nrSetresuid, nrGetresuid, nrSetresgid, nrGetresgid = 117, 118, 119, 120
	nrSetfsuid                                         = 122
	noID                                               = 0xFFFFFFFF
)

func (r *rig) res(p *proc.Process, nr uint64) [3]uint32 {
	r.t.Helper()
	if e := r.call(p, nr, scratch, scratch+4, scratch+8); e != 0 {
		r.t.Fatalf("getres = %d", e)
	}
	b := make([]byte, 12)
	p.Mem().CopyIn(scratch, b)
	return [3]uint32{le.Uint32(b), le.Uint32(b[4:]), le.Uint32(b[8:])}
}

func TestSetidRules(t *testing.T) {
	r := newRig(t)
	p := r.spawn()
	eperm := -int64(syscall.EPERM)
	for _, c := range []struct {
		what string
		nr   uint64
		args []uint64
		want int64
		uid  [3]uint32
	}{
		{"root sets all three", nrSetresuid, []uint64{1000, 1001, 1002}, 0, [3]uint32{1000, 1001, 1002}},
		{"a permutation", nrSetresuid, []uint64{1001, 1002, 1000}, 0, [3]uint32{1001, 1002, 1000}},
		{"an id not held", nrSetresuid, []uint64{5, noID, noID}, eperm, [3]uint32{1001, 1002, 1000}},
		{"setuid to the saved id: effective only", nrSetuid, []uint64{1000}, 0, [3]uint32{1001, 1000, 1000}},
		{"setuid to a stranger", nrSetuid, []uint64{1003}, eperm, [3]uint32{1001, 1000, 1000}},
		{"setreuid: real to the effective id", nrSetreuid, []uint64{1000, noID}, 0, [3]uint32{1000, 1000, 1000}},
		{"setreuid: real to a stranger", nrSetreuid, []uint64{7, noID}, eperm, [3]uint32{1000, 1000, 1000}},
		{"setuid(-1)", nrSetuid, []uint64{noID}, -int64(syscall.EINVAL), [3]uint32{1000, 1000, 1000}},
	} {
		if got := r.call(p, c.nr, c.args...); got != c.want {
			t.Errorf("%s: %d, want %d", c.what, got, c.want)
		}
		if got := r.res(p, nrGetresuid); got != c.uid {
			t.Errorf("%s: uids %v, want %v", c.what, got, c.uid)
		}
	}
	if r.call(p, nrGetuid) != 1000 || r.call(p, nrGeteuid) != 1000 {
		t.Error("getuid/geteuid")
	}
	if e := r.call(p, nrSetgroups, 0, 0); e != eperm {
		t.Errorf("a user's setgroups = %d", e)
	}
	if e := r.call(p, nrSetresgid, 1, 1, 1); e != eperm {
		t.Errorf("a user's setresgid to groups not held = %d", e)
	}
	if old := r.call(p, nrSetfsuid, 77); old != 1000 || r.call(p, nrSetfsuid, noID) != 1000 {
		t.Errorf("a user's setfsuid(77): old %d", old)
	}
}

func TestGroupsAndRoot(t *testing.T) {
	r := newRig(t)
	p := r.spawn()
	if r.call(p, nrGetuid) != 0 || r.call(p, nrGetegid) != 0 || r.call(p, nrGetgroups, 0, 0) != 0 {
		t.Fatal("a spawned process is not plain root")
	}
	b := make([]byte, 8)
	le.PutUint32(b, 10)
	le.PutUint32(b[4:], 20)
	r.put(p, scratch+64, b)
	if e := r.call(p, nrSetgroups, 2, scratch+64); e != 0 {
		t.Fatalf("setgroups = %d", e)
	}
	if n := r.call(p, nrGetgroups, 1, scratch+128); n != -int64(syscall.EINVAL) {
		t.Errorf("getgroups into a list too short = %d", n)
	}
	if n := r.call(p, nrGetgroups, 8, scratch+128); n != 2 {
		t.Fatalf("getgroups = %d", n)
	}
	got := make([]byte, 8)
	p.Mem().CopyIn(scratch+128, got)
	if le.Uint32(got) != 10 || le.Uint32(got[4:]) != 20 {
		t.Errorf("groups %v", got)
	}
	if e := r.call(p, nrSetresgid, 7, 8, 9); e != 0 || r.res(p, nrGetresgid) != [3]uint32{7, 8, 9} || r.call(p, nrGetgid) != 7 {
		t.Errorf("root's setresgid: %d %v", e, r.res(p, nrGetresgid))
	}
	if c := p.Cred(); len(c.Groups) != 2 || !c.Root() {
		t.Errorf("Cred %+v", c)
	}
}

// TestSetidExec: execve of a set-user-id and set-group-id program owned by
// 1000 by a process of user 2000: the effective and saved ids become the
// owner's, the auxiliary vector says so with AT_SECURE; a plain program
// afterwards keeps the effective ids and says AT_SECURE too, since they
// still differ from the real ones.
func TestSetidExec(t *testing.T) {
	r := newRigWith(t, fstest.MapFS{
		"bin/setid":   {Data: program(), Mode: 0o755 | fs.ModeSetuid | fs.ModeSetgid, Sys: vfs.Attr{UID: 1000, GID: 1000}},
		"bin/sgidnox": {Data: program(), Mode: 0o705 | fs.ModeSetgid, Sys: vfs.Attr{UID: 1000, GID: 1000}},
	})
	p := r.spawn()
	r.call(p, nrSetresgid, 2000, 2000, 2000)
	r.call(p, nrSetresuid, 2000, 2000, 2000)
	if e := r.call(p, 59, r.str(p, scratch, "/bin/setid"), 0, 0); e != 0 {
		t.Fatalf("execve = %d", e)
	}
	if u, g := r.res(p, nrGetresuid), r.res(p, nrGetresgid); u != [3]uint32{2000, 1000, 1000} || g != [3]uint32{2000, 1000, 1000} {
		t.Errorf("after a set-id exec: uids %v gids %v", u, g)
	}
	aux := r.auxv(p)
	if aux[11] != 2000 || aux[12] != 1000 || aux[13] != 2000 || aux[14] != 1000 || aux[23] != 1 {
		t.Errorf("auxv ids %d %d %d %d, AT_SECURE %d", aux[11], aux[12], aux[13], aux[14], aux[23])
	}
	r.call(p, nrSetresuid, 2000, 2000, 2000) // the saved id is 1000: allowed
	r.call(p, nrSetresgid, 2000, 2000, 2000)
	if e := r.call(p, 59, r.str(p, scratch, "/bin/sgidnox"), 0, 0); e != 0 || r.call(p, nrGetegid) != 2000 {
		t.Errorf("set-group-id without group execute (a locking mark) changed the egid to %d", r.call(p, nrGetegid))
	}
	if e := r.call(p, 59, r.str(p, scratch, "/bin/prog"), 0, 0); e != 0 || r.auxv(p)[23] != 0 {
		t.Errorf("a plain exec with equal ids: AT_SECURE %d", r.auxv(p)[23])
	}
}

// auxv reads the auxiliary vector of p's freshly started program.
func (r *rig) auxv(p *proc.Process) map[uint64]uint64 {
	sp := r.nu.Thread(p.TID()).RSP // where execve started the program
	word := func(i uint64) uint64 {
		b := make([]byte, 8)
		p.Mem().CopyIn(sp+8*i, b)
		return le.Uint64(b)
	}
	i := word(0) + 2 // past argc, argv and its NULL
	for word(i) != 0 {
		i++
	}
	aux := map[uint64]uint64{}
	for i++; word(i) != 0; i += 2 {
		aux[word(i)] = word(i + 1)
	}
	return aux
}

func TestKillPermission(t *testing.T) {
	r := newRig(t)
	root, user := r.spawn(), r.spawn()
	r.call(user, nrSetuid, 1000)
	other := r.spawn()
	r.call(other, nrSetresuid, 1000, 1000, 1000) // the same user as user
	eperm := -int64(syscall.EPERM)
	if e := r.call(user, 62, uint64(root.PID), 0); e != eperm {
		t.Errorf("a user probing root's process: %d", e)
	}
	if e := r.call(user, 62, uint64(other.PID), 0); e != 0 {
		t.Errorf("a user probing their own other process: %d", e)
	}
	if e := r.call(root, 62, uint64(user.PID), 0); e != 0 {
		t.Errorf("root probing a user's process: %d", e)
	}
	if e := r.call(user, 234, uint64(root.PID), uint64(root.PID), 0); e != eperm { // the main thread: tid = pid
		t.Errorf("tgkill of root's thread: %d", e)
	}
	if e := r.call(user, 62, uint64(0xFFFFFFFF), 0); e != 0 { // kill(-1): some may be signalled
		t.Errorf("kill(-1, 0) by a user: %d", e)
	}
}
