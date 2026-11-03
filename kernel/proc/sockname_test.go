package proc_test

import (
	"syscall"
	"testing"

	"burrow/kernel/proc"
)

// addr stages a struct sockaddr_un for name at va and returns its length
// (a path gets its NUL, an abstract name does not).
func (r *rig) addr(p *proc.Process, va uint64, name string) uint64 {
	b := append([]byte{1, 0}, name...)
	if name[0] != 0 {
		b = append(b, 0)
	}
	r.put(p, va, b)
	return uint64(len(b))
}

// listener binds a new stream socket to name and listens on it.
func (r *rig) listener(p *proc.Process, name string, backlog uint64) uint64 {
	r.t.Helper()
	s := uint64(r.call(p, 41, 1, sockStream, 0))
	if e := r.call(p, 49, s, scratch+3100, r.addr(p, scratch+3100, name)); e != 0 {
		r.t.Fatalf("bind %q = %d", name, e)
	}
	if e := r.call(p, 50, s, backlog); e != 0 {
		r.t.Fatalf("listen = %d", e)
	}
	return s
}

// dial connects a new stream socket (flags: SOCK_NONBLOCK...) to name.
func (r *rig) dial(p *proc.Process, name string, flags uint64) (uint64, int64) {
	c := uint64(r.call(p, 41, 1, sockStream|flags, 0))
	return c, r.call(p, 42, c, scratch+3200, r.addr(p, scratch+3200, name))
}

func TestBindListenConnectAccept(t *testing.T) {
	r, _ := vfsRig(t)
	p := r.spawn()
	l := r.listener(p, "/tmp/s", 4)
	c, e := r.dial(p, "/tmp/s", 0)
	if e != 0 {
		t.Fatalf("connect = %d", e)
	}
	r.put(p, scratch+600, []byte{110, 0, 0, 0})
	a := uint64(r.call(p, 43, l, scratch+500, scratch+600))
	if int64(a) < 0 || le.Uint32(read(p, scratch+600, 4)) != 2 {
		t.Fatalf("accept = %d, addrlen %d (an unnamed client is 2)", int64(a), le.Uint32(read(p, scratch+600, 4)))
	}
	r.str(p, scratch, "ping")
	if r.call(p, 1, c, scratch, 4); r.call(p, 0, a, scratch+100, 10) != 4 || string(read(p, scratch+100, 4)) != "ping" {
		t.Error("client to server")
	}
	r.put(p, scratch+600, []byte{110, 0, 0, 0})
	if r.call(p, 52, c, scratch+500, scratch+600); string(read(p, scratch+502, 7)) != "/tmp/s\x00" {
		t.Errorf("getpeername = %q", read(p, scratch+502, 7))
	}
	r.put(p, scratch+600, []byte{110, 0, 0, 0})
	if r.call(p, 51, a, scratch+500, scratch+600); le.Uint32(read(p, scratch+600, 4)) != 9 {
		t.Errorf("getsockname of the accepted end: length %d, want 9", le.Uint32(read(p, scratch+600, 4)))
	}
	r.put(p, scratch+600, []byte{12, 0, 0, 0})
	if e := r.call(p, 55, c, 1, 17, scratch+610, scratch+600); e != 0 || le.Uint32(read(p, scratch+610, 4)) != uint32(p.PID) {
		t.Errorf("SO_PEERCRED = %d, pid %d", e, le.Uint32(read(p, scratch+610, 4)))
	}
	r.call(p, 4, r.str(p, scratch, "/tmp/s"), scratch+700)
	if mode := le.Uint32(read(p, scratch+724, 4)); mode&0o170000 != 0o140000 {
		t.Errorf("the node's mode %#o, want S_IFSOCK", mode)
	}
	if e := r.call(p, 2, r.str(p, scratch, "/tmp/s"), 0); e != -int64(syscall.ENXIO) {
		t.Errorf("open of a socket node = %d, want ENXIO", e)
	}
	s2 := uint64(r.call(p, 41, 1, sockStream, 0))
	if e := r.call(p, 49, s2, scratch+3100, r.addr(p, scratch+3100, "/tmp/s")); e != -int64(syscall.EADDRINUSE) {
		t.Errorf("a second bind = %d, want EADDRINUSE", e)
	}
	r.call(p, 87, r.str(p, scratch, "/tmp/s")) // unlink: the name is gone
	if _, e := r.dial(p, "/tmp/s", 0); e != -int64(syscall.ENOENT) {
		t.Errorf("connect after unlink = %d, want ENOENT", e)
	}
	if e := r.call(p, 49, s2, scratch+3100, r.addr(p, scratch+3100, "/tmp/s")); e != 0 {
		t.Errorf("bind after unlink = %d", e)
	}
}

func TestAbstractNamesAndRefusals(t *testing.T) {
	r, _ := vfsRig(t)
	p := r.spawn()
	l := r.listener(p, "\x00burrow", 0)
	if _, e := r.dial(p, "\x00burrow", 0); e != 0 {
		t.Fatalf("connect to an abstract name = %d", e)
	}
	if _, e := r.dial(p, "\x00burrow", 0x800); e != -int64(syscall.EAGAIN) { // backlog 0 holds one
		t.Errorf("a non-blocking connect to a full queue = %d, want EAGAIN", e)
	}
	s := uint64(r.call(p, 41, 1, sockStream, 0))
	if e := r.call(p, 49, s, scratch+3100, r.addr(p, scratch+3100, "\x00burrow")); e != -int64(syscall.EADDRINUSE) {
		t.Errorf("abstract name taken = %d", e)
	}
	if e := r.call(p, 50, s, 1); e != -int64(syscall.EINVAL) {
		t.Errorf("listen unbound = %d, want EINVAL", e)
	}
	r.call(p, 43, l, 0, 0)     // take the waiting connection
	r.call(p, 72, l, 4, 0x800) // fcntl(F_SETFL, O_NONBLOCK) on the listener
	if e := r.call(p, 288, l, 0, 0, 0x800); e != -int64(syscall.EAGAIN) {
		t.Errorf("accept4 with nothing waiting, non-blocking listener = %d", e)
	}
	r.call(p, 3, l)
	if _, e := r.dial(p, "\x00burrow", 0); e != -int64(syscall.ECONNREFUSED) {
		t.Errorf("connect after the listener closed = %d", e)
	}
	r.str(p, scratch, "/etc/motd")
	if _, e := r.dial(p, "/etc/motd", 0); e != -int64(syscall.ECONNREFUSED) {
		t.Errorf("connect to a regular file = %d", e)
	}
	if e := r.call(p, 49, s, scratch+3100, 2); e != 0 { // autobind
		t.Errorf("autobind = %d", e)
	}
	r.put(p, scratch+600, []byte{110, 0, 0, 0})
	if r.call(p, 51, s, scratch+500, scratch+600); le.Uint32(read(p, scratch+600, 4)) != 8 || read(p, scratch+502, 1)[0] != 0 {
		t.Errorf("an autobound name: length %d", le.Uint32(read(p, scratch+600, 4)))
	}
}

// sendFDs sends one byte with SCM_RIGHTS for fds on sock.
func (r *rig) sendFDs(p *proc.Process, sock uint64, fds ...uint32) int64 {
	ctl := make([]byte, 16+4*len(fds))
	le.PutUint64(ctl, uint64(16+4*len(fds)))
	le.PutUint32(ctl[8:], 1)  // SOL_SOCKET
	le.PutUint32(ctl[12:], 1) // SCM_RIGHTS
	for i, fd := range fds {
		le.PutUint32(ctl[16+4*i:], fd)
	}
	r.put(p, scratch+1200, append(ctl, make([]byte, 8)...))
	r.put(p, scratch+1300, []byte("x"))
	iov := make([]byte, 16)
	le.PutUint64(iov, scratch+1300)
	le.PutUint64(iov[8:], 1)
	r.put(p, scratch+1400, iov)
	h := make([]byte, 56)
	le.PutUint64(h[16:], scratch+1400)
	le.PutUint64(h[24:], 1)
	le.PutUint64(h[32:], scratch+1200)
	le.PutUint64(h[40:], uint64(len(ctl)))
	r.put(p, scratch+1500, h)
	return r.call(p, 46, sock, scratch+1500, 0)
}

// recvCtl receives one message with room for controllen control bytes and
// returns the control bytes and msg_flags.
func (r *rig) recvCtl(p *proc.Process, sock, controllen uint64) ([]byte, uint32) {
	h := make([]byte, 56)
	le.PutUint64(h[16:], scratch+1400) // the iovec from sendFDs: one byte
	le.PutUint64(h[24:], 1)
	le.PutUint64(h[32:], scratch+1600)
	le.PutUint64(h[40:], controllen)
	r.put(p, scratch+1500, h)
	if n := r.call(p, 47, sock, scratch+1500, 0); n != 1 {
		r.t.Fatalf("recvmsg = %d", n)
	}
	out := read(p, scratch+1500, 56)
	return read(p, scratch+1600, int(le.Uint64(out[40:]))), le.Uint32(out[48:])
}

func TestSCMRights(t *testing.T) {
	r, _ := vfsRig(t)
	p := r.spawn()
	a, b := r.pair(p, sockStream)
	r.call(p, 22, scratch+2000) // a pipe to pass
	pr, pw := le.Uint32(read(p, scratch+2000, 4)), le.Uint32(read(p, scratch+2004, 4))
	if n := r.sendFDs(p, a, pw); n != 1 {
		t.Fatalf("sendmsg with SCM_RIGHTS = %d", n)
	}
	r.call(p, 3, uint64(pw)) // the passed reference keeps the write end open
	ctl, flags := r.recvCtl(p, b, 64)
	if flags != 0 || len(ctl) < 20 || le.Uint32(ctl[12:]) != 1 {
		t.Fatalf("control %x flags %#x", ctl, flags)
	}
	got := uint64(le.Uint32(ctl[16:]))
	r.str(p, scratch, "via fd")
	if r.call(p, 1, got, scratch, 6); r.call(p, 0, uint64(pr), scratch+100, 10) != 6 {
		t.Error("the received descriptor does not reach the pipe")
	}
	r.sendFDs(p, a, uint32(pr), uint32(pr))
	if ctl, flags := r.recvCtl(p, b, 20); flags != 0x8 || len(ctl) != 20 { // room for one fd: MSG_CTRUNC
		t.Errorf("short control buffer: %d bytes, flags %#x", len(ctl), flags)
	}
	if e := r.sendFDs(p, a, 77); e != -int64(syscall.EBADF) {
		t.Errorf("passing a closed fd = %d", e)
	}
	r.put(p, scratch+700, []byte{1, 0, 0, 0})
	r.call(p, 54, b, 1, 16, scratch+700, 4) // SO_PASSCRED
	r.sendFDs(p, a, uint32(pr))
	if ctl, _ := r.recvCtl(p, b, 64); len(ctl) < 32 || le.Uint32(ctl[12:]) != 2 || le.Uint32(ctl[16:]) != uint32(p.PID) {
		t.Errorf("SCM_CREDENTIALS first: % x", ctl)
	}
	r.call(p, 1, a, scratch+1300, 1) // a plain write carries the writer's pid too
	if ctl, _ := r.recvCtl(p, b, 64); len(ctl) < 28 || le.Uint32(ctl[16:]) != uint32(p.PID) {
		t.Errorf("credentials of a write(): % x", ctl)
	}
}
