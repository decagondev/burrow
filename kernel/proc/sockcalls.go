package proc

// sockcalls.go - the socket system calls over sock.go (W14-S06):
// socketpair, sendto/recvfrom, shutdown, getsockopt/setsockopt
// (sendmsg/recvmsg are in sockmsg.go, names in socknames.go). read, write, readv, writev,
// poll, select and epoll reach sockets through the file layer as they reach
// pipes. AF_INET and AF_INET6 sockets branch off to inetcalls.go.
// Docs: docs/02-architecture/POSIX_COVERAGE.md.

import (
	"encoding/binary"
	"syscall"
)

const (
	afUnix       = 1
	afInet       = 2
	afInet6      = 10
	sockRaw      = 3
	sockNonblock = 0x800
	sockCloexec  = 0x80000

	msgPeek     = 0x2
	msgTrunc    = 0x20
	msgDontwait = 0x40
	msgWaitall  = 0x100
	msgNosignal = 0x4000

	solSocket    = 1
	soType       = 3
	soError      = 4
	soSndbuf     = 7
	soRcvbuf     = 8
	soPasscred   = 16
	soPeercred   = 17
	soAcceptconn = 30

	msghdrBytes = 56
)

func init() {
	for nr, h := range map[uint64]handler{
		53: sysSocketpairFn, 48: sysShutdownFn, 54: sysSetsockoptFn, 55: sysGetsockoptFn,
	} {
		table[nr] = h
	}
	for nr, h := range map[uint64]thandler{
		44: sysSendtoFn, 45: sysRecvfromFn, 46: sysSendmsgFn, 47: sysRecvmsgFn,
	} {
		ttable[nr] = h
	}
}

// sockType checks socket's or socketpair's domain, type and protocol and
// returns the type (SOCK_RAW is a datagram socket, as Linux's AF_UNIX
// makes it).
func sockType(domain, typeFlags, protocol uint64) (int, int64) {
	typ := int(typeFlags) &^ (sockNonblock | sockCloexec)
	if typ == sockRaw {
		typ = sockDgram
	}
	switch {
	case domain != afUnix:
		return 0, -int64(syscall.EAFNOSUPPORT)
	case typ != sockStream && typ != sockDgram && typ != sockSeqpacket:
		return 0, -int64(syscall.ESOCKTNOSUPPORT)
	case protocol != 0 && protocol != afUnix:
		return 0, -int64(syscall.EPROTONOSUPPORT)
	}
	return typ, 0
}

// socketpair(domain, type, protocol, int sv[2]).
func sysSocketpairFn(p *Process, a [6]uint64) (int64, bool) {
	if a[0] == afInet || a[0] == afInet6 {
		return ok(-int64(syscall.EOPNOTSUPP)) // Linux: inet sockets cannot be paired
	}
	typ, e := sockType(a[0], a[1], a[2])
	if e < 0 {
		return ok(e)
	}
	s0, s1 := newSockPair(typ, p.sys, int32(p.PID))
	f0, f1 := newFile(s0, nil), newFile(s1, nil)
	nonblock := 0
	if a[1]&sockNonblock != 0 {
		nonblock = oNonblock
	}
	f0.flags, f1.flags = oRDWR|nonblock, oRDWR|nonblock
	return ok(p.installPair(f0, f1, a[1]&sockCloexec != 0, a[3]))
}

// installPair puts two new descriptions in the table and their numbers at va.
func (p *Process) installPair(f0, f1 *file, cloexec bool, va uint64) int64 {
	n0, err := p.fds.add(f0, 0, cloexec)
	if err != nil {
		f0.unref()
		f1.unref()
		return errno(err)
	}
	n1, err := p.fds.add(f1, 0, cloexec)
	if err != nil {
		p.fds.close(uint64(n0))
		f1.unref()
		return errno(err)
	}
	var b [8]byte
	binary.LittleEndian.PutUint32(b[0:], uint32(n0))
	binary.LittleEndian.PutUint32(b[4:], uint32(n1))
	if err := p.mem.CopyOut(va, b[:]); err != nil {
		p.fds.close(uint64(n0))
		p.fds.close(uint64(n1))
		return errno(err)
	}
	return 0
}

// socket is fd's socket end and description, or -EBADF/-ENOTSOCK.
func (p *Process) socket(fd uint64) (*sockEnd, *file, int64) {
	f := p.fds.get(fd)
	if f == nil {
		return nil, nil, -int64(syscall.EBADF)
	}
	s, isSock := f.f.(*sockEnd)
	if !isSock {
		return nil, nil, -int64(syscall.ENOTSOCK)
	}
	return s, f, 0
}

// sendBytes sends b, and fds with it, on fd with flags; EPIPE raises
// SIGPIPE unless MSG_NOSIGNAL. It owns fds.
func (t *Thread) sendBytes(fd uint64, b []byte, fds []*file, flags uint64, toVA uint64) int64 {
	s, f, e := t.p.socket(fd)
	switch {
	case e < 0:
	case s.pair() == nil && toVA != 0:
		e = -int64(syscall.EOPNOTSUPP) // named datagram sockets are not supported
	case s.pair() == nil:
		e = -int64(syscall.ENOTCONN)
	case toVA != 0:
		e = -int64(syscall.EISCONN)
	}
	if e < 0 {
		unrefAll(fds)
		return e
	}
	n, err := s.send(b, fds, int32(t.p.PID), f.flags&oNonblock == 0 && flags&msgDontwait == 0, t.cancel())
	if err == syscall.EPIPE && flags&msgNosignal == 0 {
		t.send(sigPIPE, sigInfo{signo: sigPIPE, code: siKernel})
	}
	if err != nil {
		return errno(err)
	}
	return int64(n)
}

// received is what one recv got: the bytes, the whole message's length,
// the descriptors that came with them (the caller owns them) and the sender.
type received struct {
	b    []byte
	full int
	fds  []*file
	pid  int32
}

// recvBytes reads at most n bytes from fd with flags, or -errno.
func (t *Thread) recvBytes(fd, n, flags uint64) (received, int64) {
	s, f, e := t.p.socket(fd)
	switch {
	case e < 0:
		return received{}, e
	case s.pair() == nil:
		return received{}, -int64(syscall.EINVAL)
	}
	b := make([]byte, min(n, maxIO))
	k, full, fds, pid, err := s.recv(b, flags&msgPeek != 0, f.flags&oNonblock == 0 && flags&msgDontwait == 0,
		flags&msgWaitall != 0, t.cancel())
	if err != nil {
		return received{}, errno(err)
	}
	return received{b[:k], full, fds, pid}, 0
}

// sendto(fd, buf, len, flags, addr, addrlen).
func sysSendtoFn(t *Thread, a [6]uint64) (int64, bool) {
	b := make([]byte, min(a[2], maxIO))
	if err := t.p.mem.CopyIn(a[1], b); err != nil {
		return ok(errno(err))
	}
	if is, f := t.p.inetFD(a[0]); is != nil {
		return ok(t.inetSend(is, f, b, a[3], a[4], a[5]))
	}
	return ok(t.sendBytes(a[0], b, nil, a[3], a[4]))
}

// recvfrom(fd, buf, len, flags, addr, addrlen *).
func sysRecvfromFn(t *Thread, a [6]uint64) (int64, bool) {
	if is, f := t.p.inetFD(a[0]); is != nil {
		return ok(t.inetRecvfrom(is, f, a))
	}
	r, e := t.recvBytes(a[0], a[2], a[3])
	if e < 0 {
		return ok(e)
	}
	unrefAll(r.fds) // recvfrom has nowhere to put them: dropped, as on Linux
	if err := t.p.mem.CopyOut(a[1], r.b); err != nil {
		return ok(errno(err))
	}
	if e := t.p.putPeerName(a[0], a[4], a[5]); e < 0 {
		return ok(e)
	}
	if a[3]&msgTrunc != 0 {
		return ok(int64(r.full)) // MSG_TRUNC: the message's real length
	}
	return ok(int64(len(r.b)))
}

// putPeerName writes fd's peer address at va (recvfrom).
func (p *Process) putPeerName(fd, va, lenVA uint64) int64 {
	s, _, e := p.socket(fd)
	if e < 0 {
		return e
	}
	s.mu.Lock()
	name := s.peerName
	s.mu.Unlock()
	return p.putSockaddr(name, va, lenVA)
}

func sysShutdownFn(p *Process, a [6]uint64) (int64, bool) {
	if is, _ := p.inetFD(a[0]); is != nil {
		return ok(is.shutdown(a[1]))
	}
	s, _, e := p.socket(a[0])
	switch {
	case e < 0:
		return ok(e)
	case a[1] > 2:
		return ok(-int64(syscall.EINVAL))
	}
	return ok(errno(s.shutdown(int(a[1]))))
}

// getsockopt(fd, level, name, value, int *len).
func sysGetsockoptFn(p *Process, a [6]uint64) (int64, bool) {
	s, e := p.optioner(a[0])
	if e < 0 {
		return ok(e)
	}
	var l [4]byte
	if err := p.mem.CopyIn(a[4], l[:]); err != nil {
		return ok(errno(err))
	}
	n := int32(binary.LittleEndian.Uint32(l[:]))
	if n < 0 {
		return ok(-int64(syscall.EINVAL))
	}
	v, e := s.option(a[1], a[2])
	if e < 0 {
		return ok(e)
	}
	k := min(int(n), len(v))
	if err := p.mem.CopyOut(a[3], v[:k]); err != nil {
		return ok(errno(err))
	}
	binary.LittleEndian.PutUint32(l[:], uint32(k))
	return ok(errno(p.mem.CopyOut(a[4], l[:])))
}

// setsockopt(fd, level, name, value, len).
func sysSetsockoptFn(p *Process, a [6]uint64) (int64, bool) {
	s, e := p.optioner(a[0])
	switch {
	case e < 0:
		return ok(e)
	case a[4] < 4:
		return ok(-int64(syscall.EINVAL))
	}
	var b [4]byte
	if err := p.mem.CopyIn(a[3], b[:]); err != nil {
		return ok(errno(err))
	}
	return ok(s.setOption(a[1], a[2], binary.LittleEndian.Uint32(b[:])))
}
