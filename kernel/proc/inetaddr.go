package proc

// inetaddr.go - struct sockaddr_in and sockaddr_in6 (W17-S03): read from
// and written to user memory for the inet socket calls. An AF_INET6 socket
// sees IPv4 peers as v4-mapped addresses (::ffff:a.b.c.d), as on Linux.
// Docs: POSIX_COVERAGE.md.

import (
	"encoding/binary"
	"syscall"

	"gvisor.dev/gvisor/pkg/tcpip"
)

const (
	afUnspec        = 0
	sockaddrInLen   = 16 // family, port, 4-byte address, 8 bytes of zero
	sockaddrIn6Len  = 28 // family, port, flow info, 16-byte address, scope
	sockaddrStorage = 128
)

// inetAddr reads the n-byte socket address at va for a socket of family.
// An unspecified address (0.0.0.0, ::) binds to every address, which
// gVisor spells as an empty one. unspec is AF_UNSPEC (connect: dissolve).
func (p *Process) inetAddr(family int, va, n uint64) (a tcpip.FullAddress, unspec bool, e int64) {
	if n < 2 || n > sockaddrStorage {
		return a, false, -int64(syscall.EINVAL)
	}
	b := make([]byte, n)
	if err := p.mem.CopyIn(va, b); err != nil {
		return a, false, errno(err)
	}
	fam := int(binary.LittleEndian.Uint16(b))
	switch {
	case fam == afUnspec:
		return a, true, 0
	case fam == afInet && n >= sockaddrInLen && (family == afInet || family == afInet6):
		a.Port = binary.BigEndian.Uint16(b[2:])
		a.Addr = tcpip.AddrFrom4Slice(b[4:8])
		if family == afInet6 { // a v4 address on a dual-stack socket
			a.Addr = tcpip.AddrFrom16Slice(append([]byte{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0xff, 0xff}, b[4:8]...))
		}
	case fam == afInet6 && n >= sockaddrIn6Len && family == afInet6:
		a.Port = binary.BigEndian.Uint16(b[2:])
		a.Addr = tcpip.AddrFrom16Slice(b[8:24])
		a.NIC = tcpip.NICID(binary.LittleEndian.Uint32(b[24:]))
	case fam != afInet && fam != afInet6:
		return a, false, -int64(syscall.EAFNOSUPPORT)
	default:
		return a, false, -int64(syscall.EINVAL)
	}
	if a.Addr.Unspecified() {
		a.Addr = tcpip.Address{}
	}
	return a, false, 0
}

// inetSockaddr is a's struct sockaddr for a socket of family.
func inetSockaddr(family int, a tcpip.FullAddress) []byte {
	ip := a.Addr.AsSlice()
	if family == afInet {
		b := make([]byte, sockaddrInLen)
		binary.LittleEndian.PutUint16(b, afInet)
		binary.BigEndian.PutUint16(b[2:], a.Port)
		if len(ip) == 4 {
			copy(b[4:], ip)
		}
		return b
	}
	b := make([]byte, sockaddrIn6Len)
	binary.LittleEndian.PutUint16(b, afInet6)
	binary.BigEndian.PutUint16(b[2:], a.Port)
	switch len(ip) {
	case 4: // an IPv4 peer of a dual-stack socket
		b[18], b[19] = 0xff, 0xff
		copy(b[20:], ip)
	case 16:
		copy(b[8:], ip)
	}
	return b
}

// putAddr writes address bytes at va (cut to the room *lenVA says) and
// their full length at lenVA, as accept, getsockname and recvfrom do.
func (p *Process) putAddr(a []byte, va, lenVA uint64) int64 {
	if va == 0 || lenVA == 0 {
		return 0
	}
	var l [4]byte
	if err := p.mem.CopyIn(lenVA, l[:]); err != nil {
		return errno(err)
	}
	room := int32(binary.LittleEndian.Uint32(l[:]))
	if room < 0 {
		return -int64(syscall.EINVAL)
	}
	if err := p.mem.CopyOut(va, a[:min(int(room), len(a))]); err != nil {
		return errno(err)
	}
	binary.LittleEndian.PutUint32(l[:], uint32(len(a)))
	return errno(p.mem.CopyOut(lenVA, l[:]))
}
