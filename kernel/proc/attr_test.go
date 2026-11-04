package proc_test

import (
	"io/fs"
	"syscall"
	"testing"
	"testing/fstest"
	"time"

	"burrow/kernel/vfs"
)

// TestStatReportsTheFileSystemsAttr (W19-S01): owners, links, the inode and
// the set-uid, set-gid and sticky bits come from the file system's
// vfs.Attr and mode; a file system without one still reports root.
func TestStatReportsTheFileSystemsAttr(t *testing.T) {
	ctime := time.Unix(1700000000, 0)
	r := newRigWith(t, fstest.MapFS{
		"srv/owned": {Data: []byte("x"), Mode: 0o750 | fs.ModeSetuid,
			Sys: vfs.Attr{UID: 1234, GID: 77, Nlink: 2, Ino: 99, Ctime: ctime, Atime: ctime}},
		"srv/shared": {Mode: fs.ModeDir | 0o777 | fs.ModeSticky | fs.ModeSetgid},
	})
	p := r.spawn()
	stat := func(name string) []byte {
		t.Helper()
		if e := r.call(p, 4, r.str(p, scratch, name), scratch+256); e != 0 {
			t.Fatalf("stat %s = %d", name, e)
		}
		st := make([]byte, 144)
		p.Mem().CopyIn(scratch+256, st)
		return st
	}
	st := stat("/srv/owned")
	if ino, nlink := le.Uint64(st[8:]), le.Uint64(st[16:]); ino != 99 || nlink != 2 {
		t.Errorf("ino %d nlink %d", ino, nlink)
	}
	if uid, gid := le.Uint32(st[28:]), le.Uint32(st[32:]); uid != 1234 || gid != 77 {
		t.Errorf("uid %d gid %d", uid, gid)
	}
	if mode := le.Uint32(st[24:]); mode != syscall.S_IFREG|syscall.S_ISUID|0o750 {
		t.Errorf("mode %o", mode)
	}
	if c := le.Uint64(st[104:]); c != uint64(ctime.Unix()) {
		t.Errorf("ctime %d", c)
	}
	if mode := le.Uint32(stat("/srv/shared")[24:]); mode != syscall.S_IFDIR|syscall.S_ISVTX|syscall.S_ISGID|0o777 {
		t.Errorf("shared: mode %o", mode)
	}
	if st := stat("/etc/motd"); le.Uint32(st[28:]) != 0 || le.Uint64(st[16:]) != 1 {
		t.Errorf("a file without Attr: uid %d nlink %d", le.Uint32(st[28:]), le.Uint64(st[16:]))
	}
}
