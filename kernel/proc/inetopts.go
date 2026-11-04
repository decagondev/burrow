package proc

// inetopts.go - getsockopt and setsockopt on inet sockets (W17-S03), as
// gVisor's socket options: SO_REUSEADDR, SO_REUSEPORT, SO_KEEPALIVE,
// SO_BROADCAST, SO_SNDBUF, SO_RCVBUF, SO_ERROR, SO_TYPE, SO_DOMAIN,
// SO_PROTOCOL, SO_ACCEPTCONN; TCP_NODELAY and the keepalive timers;
// IP_TTL; IPV6_V6ONLY. Timeouts and linger are accepted and not kept
// (Go and musl programs use poll for timeouts). Docs: POSIX_COVERAGE.md.

import (
	"encoding/binary"
	"syscall"
	"time"

	"gvisor.dev/gvisor/pkg/tcpip"
	"gvisor.dev/gvisor/pkg/tcpip/transport/tcp"
)

const (
	solIP        = 0
	solTCP       = 6
	solIPv6      = 41
	ipTTL        = 2
	ipv6Only     = 26
	tcpNodelay   = 1
	tcpMaxseg    = 2
	tcpKeepidle  = 4
	tcpKeepintvl = 5
	tcpKeepcnt   = 6

	soReuseaddr = 2
	soBroadcast = 6
	soKeepalive = 9
	soLinger    = 13
	soReuseport = 15
	soRcvtimeo  = 20
	soSndtimeo  = 21
	soProtocol  = 38
	soDomain    = 39
)

// sockOptioner is what getsockopt and setsockopt need of a socket.
type sockOptioner interface {
	option(level, name uint64) ([]byte, int64)
	setOption(level, name uint64, v uint32) int64
}

// optioner is fd's socket, whichever kind, or -EBADF/-ENOTSOCK.
func (p *Process) optioner(fd uint64) (sockOptioner, int64) {
	if s, _ := p.inetFD(fd); s != nil {
		return s, 0
	}
	s, _, e := p.socket(fd)
	if e < 0 {
		return nil, e
	}
	return s, 0
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func (s *inetSock) option(level, name uint64) ([]byte, int64) {
	so := s.ep.SocketOptions()
	var v int
	switch {
	case level == solSocket && name == soType:
		v = s.typ
	case level == solSocket && name == soDomain:
		v = s.family
	case level == solSocket && name == soProtocol:
		v = s.proto
	case level == solSocket && name == soError:
		if err := s.ep.LastError(); err != nil {
			v = int(-inetErrno(err))
		}
	case level == solSocket && name == soReuseaddr:
		v = boolInt(so.GetReuseAddress())
	case level == solSocket && name == soReuseport:
		v = boolInt(so.GetReusePort())
	case level == solSocket && name == soKeepalive:
		v = boolInt(so.GetKeepAlive())
	case level == solSocket && name == soBroadcast:
		v = boolInt(so.GetBroadcast())
	case level == solSocket && name == soSndbuf:
		v = int(so.GetSendBufferSize())
	case level == solSocket && name == soRcvbuf:
		v = int(so.GetReceiveBufferSize())
	case level == solSocket && name == soAcceptconn:
		v = boolInt(s.typ == sockStream && tcp.EndpointState(s.ep.State()) == tcp.StateListen)
	case level == solTCP && name == tcpNodelay && s.typ == sockStream:
		v = boolInt(!so.GetDelayOption())
	case level == solTCP && name == tcpMaxseg && s.typ == sockStream:
		v, _ = s.ep.GetSockOptInt(tcpip.MaxSegOption)
	case level == solIPv6 && name == ipv6Only && s.family == afInet6:
		v = boolInt(so.GetV6Only())
	default:
		return nil, -int64(syscall.ENOPROTOOPT)
	}
	var b [4]byte
	binary.LittleEndian.PutUint32(b[:], uint32(v))
	return b[:], 0
}

func (s *inetSock) setOption(level, name uint64, v uint32) int64 {
	so := s.ep.SocketOptions()
	var err tcpip.Error
	switch {
	case level == solSocket && name == soReuseaddr:
		so.SetReuseAddress(v != 0)
	case level == solSocket && name == soReuseport:
		so.SetReusePort(v != 0)
	case level == solSocket && name == soKeepalive:
		so.SetKeepAlive(v != 0)
	case level == solSocket && name == soBroadcast:
		so.SetBroadcast(v != 0)
	case level == solSocket && name == soSndbuf:
		so.SetSendBufferSize(2*int64(v), true) // Linux keeps twice what was asked
	case level == solSocket && name == soRcvbuf:
		so.SetReceiveBufferSize(2*int64(v), true)
	case level == solSocket && (name == soLinger || name == soRcvtimeo || name == soSndtimeo):
	case level == solTCP && name == tcpNodelay && s.typ == sockStream:
		so.SetDelayOption(v == 0)
	case level == solTCP && name == tcpKeepidle && s.typ == sockStream:
		o := tcpip.KeepaliveIdleOption(time.Duration(v) * time.Second)
		err = s.ep.SetSockOpt(&o)
	case level == solTCP && name == tcpKeepintvl && s.typ == sockStream:
		o := tcpip.KeepaliveIntervalOption(time.Duration(v) * time.Second)
		err = s.ep.SetSockOpt(&o)
	case level == solTCP && name == tcpKeepcnt && s.typ == sockStream:
		err = s.ep.SetSockOptInt(tcpip.KeepaliveCountOption, int(v))
	case level == solIP && name == ipTTL && s.family == afInet:
		err = s.ep.SetSockOptInt(tcpip.IPv4TTLOption, int(int32(v)))
	case level == solIPv6 && name == ipv6Only && s.family == afInet6:
		so.SetV6Only(v != 0)
	default:
		return -int64(syscall.ENOPROTOOPT)
	}
	if err != nil {
		return inetErrno(err)
	}
	return 0
}
