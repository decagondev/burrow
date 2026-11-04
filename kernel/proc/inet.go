package proc

// inet.go - AF_INET and AF_INET6 sockets (W17-S03): a file over an
// endpoint of the kernel's TCP/IP stack (gVisor's, kernel/netstack). The
// endpoint never blocks; this file waits for its readiness, so read, write,
// poll, select, epoll, O_NONBLOCK and EINTR work as for every other file.
// The socket calls (inetcalls.go) and options (inetopts.go) go to the
// endpoint; its errors become Linux's numbers (inetErrno). Never touches a
// card or a packet: the stack does. Docs: POSIX_COVERAGE.md, ADR-0009.

import (
	"bytes"
	"io/fs"
	"sync"
	"syscall"

	"gvisor.dev/gvisor/pkg/tcpip"
	"gvisor.dev/gvisor/pkg/tcpip/network/ipv4"
	"gvisor.dev/gvisor/pkg/tcpip/network/ipv6"
	"gvisor.dev/gvisor/pkg/tcpip/stack"
	"gvisor.dev/gvisor/pkg/tcpip/transport/icmp"
	"gvisor.dev/gvisor/pkg/tcpip/transport/tcp"
	"gvisor.dev/gvisor/pkg/tcpip/transport/udp"
	"gvisor.dev/gvisor/pkg/waiter"
)

const (
	ipprotoICMP   = 1
	ipprotoTCP    = 6
	ipprotoUDP    = 17
	ipprotoICMPv6 = 58

	allEvents = waiter.ReadableEvents | waiter.WritableEvents | waiter.EventErr | waiter.EventHUp | waiter.EventRdHUp
)

// inetSock is one AF_INET or AF_INET6 socket.
type inetSock struct {
	family int // afInet or afInet6
	typ    int // sockStream, sockDgram or sockRaw
	proto  int // IPPROTO_TCP, _UDP, _ICMP or _ICMPV6
	ep     tcpip.Endpoint
	wq     *waiter.Queue
	entry  waiter.Entry

	mu      sync.Mutex
	changed chan struct{} // closed and replaced on every readiness change
}

// newInetSock makes a socket's endpoint on st. A datagram socket with
// IPPROTO_ICMP is a ping socket; SOCK_RAW is ICMP or ICMPv6 only (S04:
// busybox ping), read with the IPv4 header in front as on Linux. Raw
// sockets take addresses like datagram ones.
func newInetSock(st *stack.Stack, family, typ, proto int) (*inetSock, int64) {
	netProto := tcpip.NetworkProtocolNumber(ipv4.ProtocolNumber)
	if family == afInet6 {
		netProto = ipv6.ProtocolNumber
	}
	var transport tcpip.TransportProtocolNumber
	switch {
	case typ == sockStream && (proto == 0 || proto == ipprotoTCP):
		transport, proto = tcp.ProtocolNumber, ipprotoTCP
	case typ == sockDgram && (proto == 0 || proto == ipprotoUDP):
		transport, proto = udp.ProtocolNumber, ipprotoUDP
	case typ == sockDgram && family == afInet && proto == ipprotoICMP:
		transport = icmp.ProtocolNumber4
	case typ == sockDgram && family == afInet6 && proto == ipprotoICMPv6:
		transport = icmp.ProtocolNumber6
	case typ == sockRaw && family == afInet && proto == ipprotoICMP:
		transport = icmp.ProtocolNumber4
	case typ == sockRaw && family == afInet6 && proto == ipprotoICMPv6:
		transport = icmp.ProtocolNumber6
	default:
		return nil, -int64(syscall.EPROTONOSUPPORT)
	}
	s := &inetSock{family: family, typ: typ, proto: proto, wq: &waiter.Queue{}, changed: make(chan struct{})}
	var ep tcpip.Endpoint
	var err tcpip.Error
	if typ == sockRaw {
		ep, err = st.NewRawEndpoint(transport, netProto, s.wq, true)
	} else {
		ep, err = st.NewEndpoint(transport, netProto, s.wq)
	}
	if err != nil {
		return nil, inetErrno(err)
	}
	s.ep = ep
	s.entry = waiter.NewFunctionEntry(allEvents, func(waiter.EventMask) { s.wake() })
	s.wq.EventRegister(&s.entry)
	return s, 0
}

func (s *inetSock) wake() {
	s.mu.Lock()
	close(s.changed)
	s.changed = make(chan struct{})
	s.mu.Unlock()
}

// next is the channel the next readiness change closes; take it before
// asking the endpoint, so a change in between is not missed.
func (s *inetSock) next() <-chan struct{} {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.changed
}

func (s *inetSock) Poll() (int16, <-chan struct{}) {
	ch := s.next()
	m := s.ep.Readiness(allEvents)
	var rev int16
	if m&waiter.ReadableEvents != 0 {
		rev |= pollIn
	}
	if m&waiter.WritableEvents != 0 {
		rev |= pollOut
	}
	if m&waiter.EventErr != 0 {
		rev |= pollErr
	}
	if m&waiter.EventHUp != 0 {
		rev |= pollHup
	}
	if m&waiter.EventRdHUp != 0 {
		rev |= pollRdhup
	}
	return rev, ch
}

// waitFor waits until ch closes (blocking) or says EAGAIN (not); a cancel
// is EINTR.
func waitFor(ch <-chan struct{}, block bool, cancel <-chan struct{}) error {
	if !block {
		return syscall.EAGAIN
	}
	select {
	case <-ch:
		return nil
	case <-cancel:
		return syscall.EINTR
	}
}

// recv reads at most len(b) bytes (one datagram's worth for UDP); EOF is 0.
func (s *inetSock) recv(b []byte, opts tcpip.ReadOptions, block bool, cancel <-chan struct{}) (tcpip.ReadResult, error) {
	for {
		ch := s.next()
		var buf bytes.Buffer
		res, err := s.ep.Read(&tcpip.LimitedWriter{W: &buf, N: int64(len(b))}, opts)
		switch err.(type) {
		case nil:
			copy(b, buf.Bytes())
			return res, nil
		case *tcpip.ErrClosedForReceive:
			return tcpip.ReadResult{}, nil
		case *tcpip.ErrWouldBlock:
			if err := waitFor(ch, block, cancel); err != nil {
				return tcpip.ReadResult{}, err
			}
		default:
			return tcpip.ReadResult{}, syscall.Errno(-inetErrno(err))
		}
	}
}

// send writes all of b (a stream waits for room as needed; a datagram is
// sent whole); what was written before a wait was cut short counts.
func (s *inetSock) send(b []byte, opts tcpip.WriteOptions, block bool, cancel <-chan struct{}) (int, error) {
	done := 0
	for {
		ch := s.next()
		n, err := s.ep.Write(bytes.NewReader(b[done:]), opts)
		done += int(n)
		switch err.(type) {
		case nil:
			if done == len(b) || s.typ != sockStream {
				return done, nil
			}
		case *tcpip.ErrWouldBlock:
		case *tcpip.ErrClosedForSend:
			return done, syscall.EPIPE
		default:
			return done, syscall.Errno(-inetErrno(err))
		}
		if err := waitFor(ch, block, cancel); err != nil {
			if done > 0 {
				return done, nil
			}
			return 0, err
		}
	}
}

func (s *inetSock) ReadCancel(b []byte, cancel <-chan struct{}) (int, error) {
	res, err := s.recv(b, tcpip.ReadOptions{}, true, cancel)
	return res.Count, err
}

func (s *inetSock) WriteCancel(b []byte, cancel <-chan struct{}) (int, error) {
	return s.send(b, tcpip.WriteOptions{}, true, cancel)
}

func (s *inetSock) Read(b []byte) (int, error)  { return s.ReadCancel(b, nil) }
func (s *inetSock) Write(b []byte) (int, error) { return s.WriteCancel(b, nil) }
func (s *inetSock) Stat() (fs.FileInfo, error)  { return sockInfo{}, nil }

func (s *inetSock) Close() error {
	s.wq.EventUnregister(&s.entry)
	s.ep.Close()
	return nil
}

// Ioctl: FIONREAD, the bytes waiting to be read.
func (s *inetSock) Ioctl(req, arg uint64, _, out func(va uint64, b []byte) error) (int64, error) {
	if req != fionread {
		return 0, syscall.ENOTTY
	}
	n, err := s.ep.GetSockOptInt(tcpip.ReceiveQueueSizeOption)
	if err != nil {
		n = 0
	}
	var b [4]byte
	b[0], b[1], b[2], b[3] = byte(n), byte(n>>8), byte(n>>16), byte(n>>24)
	return 0, out(arg, b[:])
}

// inetErrno is Linux's number for an endpoint's error.
func inetErrno(err tcpip.Error) int64 {
	var e syscall.Errno
	switch err.(type) {
	case *tcpip.ErrWouldBlock, *tcpip.ErrNoBufferSpace:
		e = syscall.EAGAIN
	case *tcpip.ErrConnectStarted, *tcpip.ErrAlreadyConnecting:
		e = syscall.EINPROGRESS
	case *tcpip.ErrConnectionRefused:
		e = syscall.ECONNREFUSED
	case *tcpip.ErrConnectionReset:
		e = syscall.ECONNRESET
	case *tcpip.ErrConnectionAborted, *tcpip.ErrAborted:
		e = syscall.ECONNABORTED
	case *tcpip.ErrTimeout:
		e = syscall.ETIMEDOUT
	case *tcpip.ErrPortInUse, *tcpip.ErrDuplicateAddress:
		e = syscall.EADDRINUSE
	case *tcpip.ErrBadLocalAddress:
		e = syscall.EADDRNOTAVAIL
	case *tcpip.ErrNoPortAvailable:
		e = syscall.EAGAIN
	case *tcpip.ErrNetworkUnreachable, *tcpip.ErrNoNet:
		e = syscall.ENETUNREACH
	case *tcpip.ErrHostUnreachable, *tcpip.ErrHostDown:
		e = syscall.EHOSTUNREACH
	case *tcpip.ErrAlreadyConnected:
		e = syscall.EISCONN
	case *tcpip.ErrNotConnected:
		e = syscall.ENOTCONN
	case *tcpip.ErrAlreadyBound, *tcpip.ErrInvalidEndpointState, *tcpip.ErrInvalidOptionValue:
		e = syscall.EINVAL
	case *tcpip.ErrDestinationRequired:
		e = syscall.EDESTADDRREQ
	case *tcpip.ErrMessageTooLong:
		e = syscall.EMSGSIZE
	case *tcpip.ErrClosedForSend:
		e = syscall.EPIPE
	case *tcpip.ErrBroadcastDisabled, *tcpip.ErrNotPermitted:
		e = syscall.EACCES
	case *tcpip.ErrAddressFamilyNotSupported:
		e = syscall.EAFNOSUPPORT
	case *tcpip.ErrNotSupported, *tcpip.ErrQueueSizeNotSupported:
		e = syscall.EOPNOTSUPP
	case *tcpip.ErrUnknownProtocolOption:
		e = syscall.ENOPROTOOPT
	case *tcpip.ErrBadAddress, *tcpip.ErrBadBuffer:
		e = syscall.EFAULT
	default:
		e = syscall.EINVAL
	}
	return -int64(e)
}
