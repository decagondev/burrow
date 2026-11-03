package proc_test

import (
	"syscall"
	"testing"
	"time"

	"burrow/kernel/proc"
)

const (
	sockStream, sockDgram, sockSeqpacket = 1, 2, 5
	msgPeek, msgTrunc, msgDontwait       = 0x2, 0x20, 0x40
	msgNosignal                          = 0x4000
)

// pair makes a socketpair of typ and returns its two descriptors.
func (r *rig) pair(p *proc.Process, typ uint64) (uint64, uint64) {
	r.t.Helper()
	if e := r.call(p, 53, 1, typ, 0, scratch+3000); e != 0 {
		r.t.Fatalf("socketpair(%d) = %d", typ, e)
	}
	b := read(p, scratch+3000, 8)
	return uint64(le.Uint32(b)), uint64(le.Uint32(b[4:]))
}

func TestSocketpairStream(t *testing.T) {
	r, _ := vfsRig(t)
	p := r.spawn()
	a, b := r.pair(p, sockStream)
	r.str(p, scratch, "hello")
	if n := r.call(p, 1, a, scratch, 5); n != 5 {
		t.Fatalf("write = %d", n)
	}
	r.call(p, 44, a, scratch, 3, 0, 0, 0) // sendto: "hel" again
	if n := r.call(p, 45, b, scratch+100, 3, msgPeek, 0, 0); n != 3 || string(read(p, scratch+100, 3)) != "hel" {
		t.Errorf("MSG_PEEK = %d", n)
	}
	if n := r.call(p, 0, b, scratch+100, 100); n != 8 || string(read(p, scratch+100, 8)) != "hellohel" {
		t.Errorf("a stream joins writes: %d %q", n, read(p, scratch+100, int(max(n, 0))))
	}
	if e := r.call(p, 45, b, scratch+100, 10, msgDontwait, 0, 0); e != -int64(syscall.EAGAIN) {
		t.Errorf("MSG_DONTWAIT on nothing = %d", e)
	}
	r.call(p, 48, a, 1) // shutdown(SHUT_WR): b reads end of file
	if n := r.call(p, 0, b, scratch+100, 10); n != 0 {
		t.Errorf("read after the peer's SHUT_WR = %d", n)
	}
	if e := r.call(p, 44, a, scratch, 1, msgNosignal, 0, 0); e != -int64(syscall.EPIPE) {
		t.Errorf("send after SHUT_WR = %d, want EPIPE", e)
	}
}

func TestSocketClosedPeer(t *testing.T) {
	r, _ := vfsRig(t)
	p := r.spawn()
	a, b := r.pair(p, sockStream)
	r.call(p, 3, b)
	if n := r.call(p, 0, a, scratch, 10); n != 0 {
		t.Errorf("read from a closed peer = %d, want 0", n)
	}
	if e := r.call(p, 44, a, scratch, 1, msgNosignal, 0, 0); e != -int64(syscall.EPIPE) {
		t.Errorf("send with MSG_NOSIGNAL = %d, want EPIPE", e)
	}
	r.nu.Inject(proc.Request{TID: p.TID(), Nr: 1, Args: [6]uint64{a, scratch, 1}})
	if st, _ := wait(t, p); st != 128+13 {
		t.Errorf("write to a closed peer: status %d, want SIGPIPE", st)
	}
}

func TestSocketpairDatagrams(t *testing.T) {
	for _, typ := range []uint64{sockDgram, sockSeqpacket} {
		r, _ := vfsRig(t)
		p := r.spawn()
		a, b := r.pair(p, typ)
		r.str(p, scratch, "first message")
		r.call(p, 1, a, scratch, 5)  // "first"
		r.call(p, 1, a, scratch, 13) // "first message"
		if n := r.call(p, 0, b, scratch+100, 100); n != 5 {
			t.Errorf("type %d: boundaries kept: read %d, want 5", typ, n)
		}
		if n := r.call(p, 45, b, scratch+100, 3, msgTrunc, 0, 0); n != 13 {
			t.Errorf("type %d: MSG_TRUNC = %d, want 13 (the whole length)", typ, n)
		}
		if e := r.call(p, 45, b, scratch+100, 10, msgDontwait, 0, 0); e != -int64(syscall.EAGAIN) {
			t.Errorf("type %d: the rest of a cut message is gone: %d", typ, e)
		}
	}
}

func TestRecvmsgAndOptions(t *testing.T) {
	r, _ := vfsRig(t)
	p := r.spawn()
	a, b := r.pair(p, sockDgram)
	r.str(p, scratch, "0123456789")
	r.call(p, 1, a, scratch, 10)
	// struct iovec {scratch+200, 4}, {scratch+300, 2}; msghdr at scratch+400
	iov := make([]byte, 32)
	le.PutUint64(iov[0:], scratch+200)
	le.PutUint64(iov[8:], 4)
	le.PutUint64(iov[16:], scratch+300)
	le.PutUint64(iov[24:], 2)
	r.put(p, scratch+500, iov)
	h := make([]byte, 56)
	le.PutUint64(h[16:], scratch+500)
	le.PutUint64(h[24:], 2)
	le.PutUint64(h[40:], 99) // controllen: zeroed on return
	r.put(p, scratch+400, h)
	if n := r.call(p, 47, b, scratch+400, 0); n != 6 || string(read(p, scratch+200, 4)) != "0123" || string(read(p, scratch+300, 2)) != "45" {
		t.Errorf("recvmsg = %d", n)
	}
	out := read(p, scratch+400, 56)
	if le.Uint32(out[48:]) != msgTrunc || le.Uint64(out[40:]) != 0 {
		t.Errorf("msg_flags %#x controllen %d", le.Uint32(out[48:]), le.Uint64(out[40:]))
	}
	opt := func(name uint64) uint32 {
		r.put(p, scratch+600, []byte{4, 0, 0, 0})
		if e := r.call(p, 55, a, 1, name, scratch+610, scratch+600); e != 0 {
			t.Fatalf("getsockopt(%d) = %d", name, e)
		}
		return le.Uint32(read(p, scratch+610, 4))
	}
	if opt(3) != sockDgram || opt(4) != 0 || opt(7) != 212992 {
		t.Errorf("SO_TYPE %d SO_ERROR %d SO_SNDBUF %d", opt(3), opt(4), opt(7))
	}
	r.put(p, scratch+620, []byte{0, 0x10, 0, 0}) // 4096
	if e := r.call(p, 54, a, 1, 8, scratch+620, 4); e != 0 || opt(8) != 8192 {
		t.Errorf("SO_RCVBUF 4096 reads back %d, want 8192 (doubled, as Linux)", opt(8))
	}
	r.call(p, 5, a, scratch+700) // fstat
	if mode := le.Uint32(read(p, scratch+724, 4)); mode&0o170000 != 0o140000 {
		t.Errorf("st_mode %#o, want S_IFSOCK", mode)
	}
}

func TestSocketReadinessAndErrors(t *testing.T) {
	r, _ := vfsRig(t)
	p := r.spawn()
	a, b := r.pair(p, sockStream|0x800) // SOCK_NONBLOCK
	if e := r.call(p, 0, b, scratch, 1); e != -int64(syscall.EAGAIN) {
		t.Errorf("SOCK_NONBLOCK read = %d", e)
	}
	fds := uint64(scratch + 256)
	r.pollfds(p, fds, [2]int{int(b), pollIn | 0x2000}, [2]int{int(a), pollOut})
	if n := r.call(p, 7, fds, 2, 0); n != 1 || revents(p, fds, 0) != 0 || revents(p, fds, 1) != pollOut {
		t.Errorf("idle pair: %d, %#x %#x", n, revents(p, fds, 0), revents(p, fds, 1))
	}
	r.call(p, 1, a, scratch, 3)
	r.call(p, 0x10, b, 0x541B, scratch+800) // FIONREAD
	if n := le.Uint32(read(p, scratch+800, 4)); n != 3 {
		t.Errorf("FIONREAD = %d", n)
	}
	r.call(p, 3, a)
	if r.call(p, 7, fds, 1, 0); revents(p, fds, 0) != pollIn|pollHup|0x2000 {
		t.Errorf("peer gone: revents %#x, want POLLIN|POLLHUP|POLLRDHUP", revents(p, fds, 0))
	}
	if e := r.call(p, 53, 2, sockStream, 0, scratch); e != -int64(syscall.EOPNOTSUPP) {
		t.Errorf("AF_INET pair = %d", e)
	}
	if e := r.call(p, 53, 16, sockStream, 0, scratch); e != -int64(syscall.EAFNOSUPPORT) {
		t.Errorf("AF_NETLINK pair = %d", e)
	}
	if e := r.call(p, 53, 1, 4, 0, scratch); e != -int64(syscall.ESOCKTNOSUPPORT) {
		t.Errorf("SOCK_RDM pair = %d", e)
	}
	if e := r.call(p, 48, 1, 0); e != -int64(syscall.ENOTSOCK) {
		t.Errorf("shutdown on the console = %d", e)
	}
}

// A blocked read wakes when the other end writes from another process.
func TestSocketAcrossFork(t *testing.T) {
	r, _ := vfsRig(t)
	p := r.spawn()
	a, b := r.pair(p, sockStream)
	r.str(p, scratch, "ping") // before the fork: the child's copy has it
	r.call(p, 57)
	kid := r.newestThread(p.TID())
	r.nu.Inject(proc.Request{TID: p.TID(), Nr: 0, Args: [6]uint64{b, scratch + 100, 10}})
	time.Sleep(10 * time.Millisecond)
	if n := r.callAs(kid, 1, a, scratch, 4); n != 4 {
		t.Fatalf("child write = %d", n)
	}
	if n := r.await(p.TID()); n != 4 || string(read(p, scratch+100, 4)) != "ping" {
		t.Errorf("parent read = %d", n)
	}
}
