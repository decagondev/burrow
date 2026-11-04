package proc

// inetmsg.go - recvfrom and recvmsg on inet sockets (W17-S03): the bytes
// into the caller's buffers, the sender's address, MSG_TRUNC with a
// datagram's real length; no control messages (msg_controllen comes back
// 0). Docs: POSIX_COVERAGE.md.

import (
	"encoding/binary"

	"gvisor.dev/gvisor/pkg/tcpip/stack"
)

// network is the stack sockets are made on, or nil before it started.
func (s *System) network() *stack.Stack {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.net
}

// SetNetwork gives the system the kernel's TCP/IP stack (W17-S03); until
// then socket(AF_INET...) is EAFNOSUPPORT.
func (s *System) SetNetwork(st *stack.Stack) {
	s.mu.Lock()
	s.net = st
	s.mu.Unlock()
}

// recvfrom(fd, buf, len, flags, addr, addrlen *).
func (t *Thread) inetRecvfrom(s *inetSock, f *file, a [6]uint64) int64 {
	b, total, from, e := t.inetRecv(s, f, a[2], a[3])
	if e < 0 {
		return e
	}
	if err := t.p.mem.CopyOut(a[1], b); err != nil {
		return errno(err)
	}
	if e := t.p.putAddr(inetSockaddr(s.family, from), a[4], a[5]); e < 0 {
		return e
	}
	if a[3]&msgTrunc != 0 {
		return int64(total)
	}
	return int64(len(b))
}

// recvmsg(fd, msghdr, flags) with h, the msghdr's bytes raw, and its iovecs.
func (t *Thread) inetRecvmsg(s *inetSock, f *file, a [6]uint64, h msghdr, raw []byte, iov [][2]uint64, n uint64) int64 {
	p := t.p
	b, total, from, e := t.inetRecv(s, f, n, a[2])
	if e < 0 {
		return e
	}
	rest := b
	for _, v := range iov {
		k := min(uint64(len(rest)), v[1])
		if err := p.mem.CopyOut(v[0], rest[:k]); err != nil {
			return errno(err)
		}
		rest = rest[k:]
	}
	if h.name != 0 { // msg_namelen is the room in, the length out
		if e := p.putAddr(inetSockaddr(s.family, from), h.name, a[1]+8); e < 0 {
			return e
		}
	}
	flags := uint32(0)
	if total > len(b) {
		flags |= msgTrunc
	}
	binary.LittleEndian.PutUint64(raw[40:], 0) // no control messages
	binary.LittleEndian.PutUint32(raw[48:], flags)
	if err := p.mem.CopyOut(a[1]+40, raw[40:]); err != nil {
		return errno(err)
	}
	if a[2]&msgTrunc != 0 {
		return int64(total)
	}
	return int64(len(b))
}
