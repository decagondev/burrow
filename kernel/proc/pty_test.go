package proc_test

import (
	"syscall"
	"testing"
)

// The calls musl's openpty and st make, through the system call table.
func TestPseudoTerminal(t *testing.T) {
	r := newRig(t)
	p := r.spawn()
	const rdwr = 2
	m := uint64(r.call(p, 2, r.str(p, scratch, "/dev/ptmx"), rdwr))
	if int64(m) < 0 {
		t.Fatalf("open /dev/ptmx = %d", int64(m))
	}
	if e := r.call(p, 16, m, 0x40045431, scratch+512); e != 0 { // TIOCSPTLCK
		t.Errorf("TIOCSPTLCK = %d", e)
	}
	if e := r.call(p, 16, m, 0x80045430, scratch+512); e != 0 || le.Uint32(read(p, scratch+512, 4)) != 0 {
		t.Fatalf("TIOCGPTN = %d, n %d", e, le.Uint32(read(p, scratch+512, 4)))
	}
	s := uint64(r.call(p, 2, r.str(p, scratch, "/dev/pts/0"), rdwr))
	if int64(s) < 0 {
		t.Fatalf("open /dev/pts/0 = %d", int64(s))
	}
	if e := r.call(p, 2, r.str(p, scratch, "/dev/pts/5"), rdwr); e != -int64(syscall.ENOENT) {
		t.Errorf("open /dev/pts/5 = %d, want ENOENT", e)
	}
	r.put(p, scratch, []byte("hi\n"))
	r.call(p, 1, m, scratch, 3)
	if n := r.call(p, 0, s, scratch+256, 64); string(read(p, scratch+256, int(n))) != "hi\n" {
		t.Errorf("slave read %q", read(p, scratch+256, int(max(n, 0))))
	}
	if n := r.call(p, 0, m, scratch+256, 64); string(read(p, scratch+256, int(n))) != "hi\r\n" {
		t.Errorf("master read the echo %q", read(p, scratch+256, int(max(n, 0))))
	}
	ws := make([]byte, 8)
	le.PutUint16(ws[0:], 30)
	le.PutUint16(ws[2:], 100)
	r.put(p, scratch+512, ws)
	r.call(p, 16, m, 0x5414, scratch+512) // TIOCSWINSZ on the master
	if r.call(p, 16, s, 0x5413, scratch+600); le.Uint16(read(p, scratch+602, 2)) != 100 {
		t.Errorf("the slave's size after the master set it: %d cols", le.Uint16(read(p, scratch+602, 2)))
	}
	if e := r.call(p, 16, s, 0x540E, 0); e != 0 { // TIOCSCTTY
		t.Errorf("TIOCSCTTY = %d", e)
	}
	tty := uint64(r.call(p, 2, r.str(p, scratch, "/dev/tty"), rdwr))
	r.put(p, scratch, []byte("via tty"))
	r.call(p, 1, tty, scratch, 7)
	if n := r.call(p, 0, m, scratch+256, 64); string(read(p, scratch+256, int(n))) != "via tty" {
		t.Errorf("/dev/tty after TIOCSCTTY is not the slave: master read %q", read(p, scratch+256, int(max(n, 0))))
	}
	r.call(p, 3, s)
	r.call(p, 3, tty)
	if e := r.call(p, 0, m, scratch+256, 64); e != -int64(syscall.EIO) {
		t.Errorf("master read with the slave closed = %d, want EIO", e)
	}
}

// BUG-0019: musl's ioctl takes the request as an int, so _IOR requests (the
// top bit set) arrive sign-extended; Linux reads only the low 32 bits.
func TestIoctlRequestIs32Bits(t *testing.T) {
	r := newRig(t)
	p := r.spawn()
	m := uint64(r.call(p, 2, r.str(p, scratch, "/dev/ptmx"), 2))
	if e := r.call(p, 16, m, 0xFFFFFFFF80045430, scratch+512); e != 0 { // TIOCGPTN as musl passes it
		t.Errorf("sign-extended TIOCGPTN = %d, want 0", e)
	}
}

// A slave belongs to whoever opened /dev/ptmx, mode 0620; root gives it
// away as dropbear does, its owner changes its mode, others may do neither
// (W24-S03). fstat of the slave says what stat of its name says.
func TestPseudoTerminalOwner(t *testing.T) {
	r := newRig(t)
	p := r.spawn()
	const rdwr, statBuf = 2, scratch + 1024
	m := uint64(r.call(p, 2, r.str(p, scratch, "/dev/ptmx"), rdwr))
	stat := func() (uid, gid, mode uint32, ino uint64) {
		if e := r.call(p, 4, r.str(p, scratch, "/dev/pts/0"), statBuf); e != 0 {
			t.Fatalf("stat /dev/pts/0 = %d", e)
		}
		b := read(p, statBuf, 144)
		return le.Uint32(b[28:]), le.Uint32(b[32:]), le.Uint32(b[24:]), le.Uint64(b[8:])
	}
	if uid, _, mode, _ := stat(); uid != 0 || mode != 0o20620 {
		t.Fatalf("root's new slave: uid %d mode %o, want 0 and 20620", uid, mode)
	}
	if e := r.call(p, 92, r.str(p, scratch, "/dev/pts/0"), 1000, 5); e != 0 {
		t.Fatalf("root's chown of the slave = %d", e)
	}
	if uid, gid, _, _ := stat(); uid != 1000 || gid != 5 {
		t.Fatalf("after chown: uid %d gid %d", uid, gid)
	}
	s := uint64(r.call(p, 2, r.str(p, scratch, "/dev/pts/0"), rdwr))
	_, _, _, ino := stat()
	r.call(p, 5, s, statBuf) // fstat
	if b := read(p, statBuf, 144); le.Uint64(b[8:]) != ino || le.Uint32(b[28:]) != 1000 {
		t.Errorf("fstat of the slave: ino %x uid %d, stat said ino %x uid 1000", le.Uint64(b[8:]), le.Uint32(b[28:]), ino)
	}
	r.call(p, 119, 1000, 1000, 1000) // setresgid
	r.call(p, 117, 1000, 1000, 1000) // setresuid: now the owner, not root
	if e := r.call(p, 90, r.str(p, scratch, "/dev/pts/0"), 0o600); e != 0 {
		t.Errorf("the owner's chmod = %d", e)
	}
	if _, _, mode, _ := stat(); mode != 0o20600 {
		t.Errorf("after chmod: mode %o", mode)
	}
	if e := r.call(p, 92, r.str(p, scratch, "/dev/pts/0"), 0, 0); e != -int64(syscall.EPERM) {
		t.Errorf("a user's chown to root = %d, want EPERM", e)
	}
	r.call(p, 3, s)
	r.call(p, 3, m)
	if e := r.call(p, 92, r.str(p, scratch, "/dev/pts/0"), 1000, 1000); e != -int64(syscall.ENOENT) {
		t.Errorf("chown of a closed pair = %d, want ENOENT", e)
	}
}
