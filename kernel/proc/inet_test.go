package proc_test

import (
	"encoding/binary"
	"syscall"
	"testing"

	"burrow/kernel/netstack"
	"burrow/kernel/proc"
)

const (
	afInet, afInet6 = 2, 10
	sockNonblockF   = 0x800
	einprogress     = -int64(syscall.EINPROGRESS)
)

// netRig is a rig whose system has a loopback-only TCP/IP stack.
func netRig(t *testing.T) *rig {
	r := newRig(t)
	st, err := netstack.New(nil, netstack.Config{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(st.Close)
	r.sys.SetNetwork(st.Stack)
	return r
}

// in4 stages a struct sockaddr_in for ip:port at va and returns its length.
func (r *rig) in4(p *proc.Process, va uint64, ip [4]byte, port uint16) uint64 {
	b := make([]byte, 16)
	le.PutUint16(b, afInet)
	binary.BigEndian.PutUint16(b[2:], port)
	copy(b[4:], ip[:])
	r.put(p, va, b)
	return 16
}

// in6 stages a struct sockaddr_in6 for ip:port at va.
func (r *rig) in6(p *proc.Process, va uint64, ip [16]byte, port uint16) uint64 {
	b := make([]byte, 28)
	le.PutUint16(b, afInet6)
	binary.BigEndian.PutUint16(b[2:], port)
	copy(b[8:], ip[:])
	r.put(p, va, b)
	return 28
}

var localhost = [4]byte{127, 0, 0, 1}

// tcpListener makes a socket listening on 127.0.0.1:port.
func (r *rig) tcpListener(p *proc.Process, port uint16) uint64 {
	r.t.Helper()
	l := uint64(r.call(p, 41, afInet, sockStream, 0))
	r.put(p, scratch+3000, []byte{1, 0, 0, 0})
	if e := r.call(p, 54, l, 1, 2, scratch+3000, 4); e != 0 { // SO_REUSEADDR
		r.t.Fatalf("setsockopt = %d", e)
	}
	if e := r.call(p, 49, l, scratch+3100, r.in4(p, scratch+3100, localhost, port)); e != 0 {
		r.t.Fatalf("bind = %d", e)
	}
	if e := r.call(p, 50, l, 8); e != 0 {
		r.t.Fatalf("listen = %d", e)
	}
	return l
}

func (r *rig) intOpt(p *proc.Process, fd, level, name uint64) uint32 {
	r.put(p, scratch+3300, []byte{4, 0, 0, 0})
	if e := r.call(p, 55, fd, level, name, scratch+3310, scratch+3300); e != 0 {
		r.t.Fatalf("getsockopt(%d, %d) = %d", level, name, e)
	}
	return le.Uint32(read(p, scratch+3310, 4))
}

func TestInetTCPLoopback(t *testing.T) {
	r := netRig(t)
	p := r.spawn()
	l := r.tcpListener(p, 7000)
	c := uint64(r.call(p, 41, afInet, sockStream, 0))
	if e := r.call(p, 42, c, scratch+3200, r.in4(p, scratch+3200, localhost, 7000)); e != 0 {
		t.Fatalf("connect = %d", e)
	}
	r.put(p, scratch+600, []byte{16, 0, 0, 0})
	a := uint64(r.call(p, 43, l, scratch+500, scratch+600))
	if int64(a) < 0 || le.Uint16(read(p, scratch+500, 2)) != afInet || string(read(p, scratch+504, 4)) != string(localhost[:]) {
		t.Fatalf("accept = %d, peer % x", int64(a), read(p, scratch+500, 16))
	}
	r.str(p, scratch, "ping over tcp")
	if n := r.call(p, 1, c, scratch, 13); n != 13 {
		t.Fatalf("write = %d", n)
	}
	if n := r.call(p, 0, a, scratch+100, 64); n != 13 || string(read(p, scratch+100, 13)) != "ping over tcp" {
		t.Fatalf("read = %d %q", n, read(p, scratch+100, 13))
	}
	r.put(p, scratch+600, []byte{16, 0, 0, 0})
	if r.call(p, 52, c, scratch+500, scratch+600); binary.BigEndian.Uint16(read(p, scratch+502, 2)) != 7000 {
		t.Errorf("getpeername port %d", binary.BigEndian.Uint16(read(p, scratch+502, 2)))
	}
	if r.intOpt(p, c, 1, 3) != sockStream || r.intOpt(p, l, 1, 30) != 1 || r.intOpt(p, c, 1, 30) != 0 {
		t.Error("SO_TYPE or SO_ACCEPTCONN")
	}
	r.put(p, scratch+3000, []byte{1, 0, 0, 0})
	if e := r.call(p, 54, c, 6, 1, scratch+3000, 4); e != 0 || r.intOpt(p, c, 6, 1) != 1 {
		t.Errorf("TCP_NODELAY = %d", e)
	}
	if e := r.call(p, 48, c, 1); e != 0 { // SHUT_WR: the server reads the end
		t.Fatalf("shutdown = %d", e)
	}
	if n := r.call(p, 0, a, scratch+100, 64); n != 0 {
		t.Errorf("read after the peer's shutdown = %d, want 0", n)
	}
	r.str(p, scratch, "reply")
	if r.call(p, 1, a, scratch, 5); r.call(p, 0, c, scratch+100, 64) != 5 {
		t.Error("the other direction after SHUT_WR")
	}
}

func TestInetRefusedAndNonblockingConnect(t *testing.T) {
	r := netRig(t)
	p := r.spawn()
	c := uint64(r.call(p, 41, afInet, sockStream, 0))
	if e := r.call(p, 42, c, scratch+3200, r.in4(p, scratch+3200, localhost, 7001)); e != -int64(syscall.ECONNREFUSED) {
		t.Errorf("connect to a closed port = %d, want ECONNREFUSED", e)
	}
	n := uint64(r.call(p, 41, afInet, sockStream|sockNonblockF, 0))
	if e := r.call(p, 42, n, scratch+3200, r.in4(p, scratch+3200, localhost, 7001)); e != einprogress && e != -int64(syscall.ECONNREFUSED) {
		t.Fatalf("non-blocking connect = %d, want EINPROGRESS", e)
	}
	r.pollfds(p, scratch+800, [2]int{int(n), pollOut})
	r.call(p, 7, scratch+800, 1, 2000)
	if got := r.intOpt(p, n, 1, 4); got != uint32(syscall.ECONNREFUSED) {
		t.Errorf("SO_ERROR = %d, want ECONNREFUSED", got)
	}
	l := r.tcpListener(p, 7002)
	m := uint64(r.call(p, 41, afInet, sockStream|sockNonblockF, 0))
	r.call(p, 42, m, scratch+3200, r.in4(p, scratch+3200, localhost, 7002))
	r.pollfds(p, scratch+800, [2]int{int(m), pollOut})
	if k := r.call(p, 7, scratch+800, 1, 2000); k != 1 || r.intOpt(p, m, 1, 4) != 0 {
		t.Errorf("non-blocking connect to a listener: poll %d, SO_ERROR %d", k, r.intOpt(p, m, 1, 4))
	}
	if a := r.call(p, 288, l, 0, 0, sockNonblockF); a < 0 {
		t.Errorf("accept4 = %d", a)
	}
	r.call(p, 72, l, 4, sockNonblockF) // fcntl(F_SETFL, O_NONBLOCK): accept4's flags are the new socket's
	if a := r.call(p, 288, l, 0, 0, 0); a != -int64(syscall.EAGAIN) {
		t.Errorf("accept4 on an empty non-blocking listener = %d, want EAGAIN", a)
	}
}

func TestInetUDP(t *testing.T) {
	r := netRig(t)
	p := r.spawn()
	u := uint64(r.call(p, 41, afInet, sockDgram, 0))
	if e := r.call(p, 49, u, scratch+3100, r.in4(p, scratch+3100, localhost, 7100)); e != 0 {
		t.Fatalf("bind = %d", e)
	}
	v := uint64(r.call(p, 41, afInet, sockDgram, 0))
	r.str(p, scratch, "hello udp")
	if n := r.call(p, 44, v, scratch, 9, 0, scratch+3200, r.in4(p, scratch+3200, localhost, 7100)); n != 9 {
		t.Fatalf("sendto = %d", n)
	}
	r.put(p, scratch+600, []byte{16, 0, 0, 0})
	if n := r.call(p, 45, u, scratch+100, 5, 0x20, scratch+500, scratch+600); n != 9 { // MSG_TRUNC
		t.Errorf("recvfrom with MSG_TRUNC = %d, want the datagram's 9", n)
	}
	if string(read(p, scratch+100, 5)) != "hello" || string(read(p, scratch+504, 4)) != string(localhost[:]) {
		t.Errorf("recvfrom got %q from % x", read(p, scratch+100, 5), read(p, scratch+500, 8))
	}
	u2 := uint64(r.call(p, 41, afInet, sockDgram, 0))
	if e := r.call(p, 49, u2, scratch+3100, r.in4(p, scratch+3100, localhost, 7100)); e != -int64(syscall.EADDRINUSE) {
		t.Errorf("a second bind = %d, want EADDRINUSE", e)
	}
	if n := r.call(p, 45, u, scratch+100, 5, 0x40, 0, 0); n != -int64(syscall.EAGAIN) { // MSG_DONTWAIT
		t.Errorf("recvfrom on an empty socket, MSG_DONTWAIT = %d", n)
	}
}

func TestInetIPv6AndErrors(t *testing.T) {
	r := netRig(t)
	p := r.spawn()
	l := uint64(r.call(p, 41, afInet6, sockStream, 0))
	lo6 := [16]byte{15: 1}
	if e := r.call(p, 49, l, scratch+3100, r.in6(p, scratch+3100, lo6, 7200)); e != 0 {
		t.Fatalf("bind ::1 = %d", e)
	}
	r.call(p, 50, l, 4)
	c := uint64(r.call(p, 41, afInet6, sockStream, 0))
	if e := r.call(p, 42, c, scratch+3200, r.in6(p, scratch+3200, lo6, 7200)); e != 0 {
		t.Fatalf("connect [::1] = %d", e)
	}
	if a := r.call(p, 43, l, 0, 0); a < 0 {
		t.Fatalf("accept = %d", a)
	}
	if e := r.call(p, 41, afInet, 5, 0); e != -int64(syscall.ESOCKTNOSUPPORT) {
		t.Errorf("SOCK_SEQPACKET = %d", e)
	}
	if e := r.call(p, 41, afInet, sockStream, 17); e != -int64(syscall.EPROTONOSUPPORT) {
		t.Errorf("a stream with IPPROTO_UDP = %d", e)
	}
	s := uint64(r.call(p, 41, afInet, sockStream, 0))
	if n := r.call(p, 0, s, scratch, 4); n != -int64(syscall.ENOTCONN) {
		t.Errorf("read on an unconnected socket = %d, want ENOTCONN", n)
	}
	bare := newRig(t) // a system with no network
	if e := bare.call(bare.spawn(), 41, afInet, sockStream, 0); e != -int64(syscall.EAFNOSUPPORT) {
		t.Errorf("socket without a network = %d", e)
	}
}
