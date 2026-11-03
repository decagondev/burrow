package proc

// sockcalls.go - the socket system calls over sock.go (W14-S06):
// socketpair, sendto/recvfrom, sendmsg/recvmsg (no control messages yet),
// shutdown, getsockopt/setsockopt and FIONREAD. read, write, readv, writev,
// poll, select and epoll reach sockets through the file layer as they reach
// pipes. Only AF_UNIX; a peer address, where asked for, is unnamed.
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

	solSocket = 1
	soType    = 3
	soError   = 4
	soSndbuf  = 7
	soRcvbuf  = 8

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

// socketpair(domain, type, protocol, int sv[2]).
func sysSocketpairFn(p *Process, a [6]uint64) (int64, bool) {
	typ := int(a[1]) &^ (sockNonblock | sockCloexec)
	if typ == sockRaw {
		typ = sockDgram // as Linux's AF_UNIX does
	}
	switch {
	case a[0] == afInet || a[0] == afInet6:
		return ok(-int64(syscall.EOPNOTSUPP)) // Linux: inet sockets cannot be paired
	case a[0] != afUnix:
		return ok(-int64(syscall.EAFNOSUPPORT))
	case typ != sockStream && typ != sockDgram && typ != sockSeqpacket:
		return ok(-int64(syscall.ESOCKTNOSUPPORT))
	case a[2] != 0 && a[2] != afUnix:
		return ok(-int64(syscall.EPROTONOSUPPORT))
	}
	s0, s1 := newSockPair(typ)
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

// sendBytes sends b on fd with flags; EPIPE raises SIGPIPE unless MSG_NOSIGNAL.
func (t *Thread) sendBytes(fd uint64, b []byte, flags uint64, toVA uint64) int64 {
	s, f, e := t.p.socket(fd)
	if e < 0 {
		return e
	}
	if toVA != 0 {
		return -int64(syscall.EISCONN) // a socketpair end is connected; names are W14-S07
	}
	n, err := s.send(b, f.flags&oNonblock == 0 && flags&msgDontwait == 0, t.cancel())
	if err == syscall.EPIPE && flags&msgNosignal == 0 {
		t.send(sigPIPE, sigInfo{signo: sigPIPE, code: siKernel})
	}
	if err != nil {
		return errno(err)
	}
	return int64(n)
}

// recvBytes reads at most n bytes from fd with flags: the bytes, the whole
// message's length (recv), or -errno.
func (t *Thread) recvBytes(fd, n, flags uint64) ([]byte, int, int64) {
	s, f, e := t.p.socket(fd)
	if e < 0 {
		return nil, 0, e
	}
	b := make([]byte, min(n, maxIO))
	k, full, err := s.recv(b, flags&msgPeek != 0, f.flags&oNonblock == 0 && flags&msgDontwait == 0,
		flags&msgWaitall != 0, t.cancel())
	if err != nil {
		return nil, 0, errno(err)
	}
	return b[:k], full, 0
}

// sendto(fd, buf, len, flags, addr, addrlen).
func sysSendtoFn(t *Thread, a [6]uint64) (int64, bool) {
	b := make([]byte, min(a[2], maxIO))
	if err := t.p.mem.CopyIn(a[1], b); err != nil {
		return ok(errno(err))
	}
	return ok(t.sendBytes(a[0], b, a[3], a[4]))
}

// recvfrom(fd, buf, len, flags, addr, addrlen *).
func sysRecvfromFn(t *Thread, a [6]uint64) (int64, bool) {
	b, full, e := t.recvBytes(a[0], a[2], a[3])
	if e < 0 {
		return ok(e)
	}
	if err := t.p.mem.CopyOut(a[1], b); err != nil {
		return ok(errno(err))
	}
	if e := t.p.putPeerName(a[4], a[5]); e < 0 {
		return ok(e)
	}
	if a[3]&msgTrunc != 0 {
		return ok(int64(full)) // MSG_TRUNC: the message's real length
	}
	return ok(int64(len(b)))
}

// putPeerName writes an unnamed AF_UNIX address (just the family) at va,
// if lenVA says there is room, and its length (2) at lenVA.
func (p *Process) putPeerName(va, lenVA uint64) int64 {
	if va == 0 || lenVA == 0 {
		return 0
	}
	var l [4]byte
	if err := p.mem.CopyIn(lenVA, l[:]); err != nil {
		return errno(err)
	}
	if int32(binary.LittleEndian.Uint32(l[:])) >= 2 {
		if err := p.mem.CopyOut(va, []byte{afUnix, 0}); err != nil {
			return errno(err)
		}
	}
	binary.LittleEndian.PutUint32(l[:], 2)
	return errno(p.mem.CopyOut(lenVA, l[:]))
}

func sysShutdownFn(p *Process, a [6]uint64) (int64, bool) {
	s, _, e := p.socket(a[0])
	switch {
	case e < 0:
		return ok(e)
	case a[1] > 2:
		return ok(-int64(syscall.EINVAL))
	}
	s.shutdown(int(a[1]))
	return ok(0)
}
