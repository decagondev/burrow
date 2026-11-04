package proc

// inetcalls.go - the socket calls on AF_INET and AF_INET6 sockets
// (W17-S03): socket, bind, listen, connect (EINPROGRESS without blocking,
// the outcome then in SO_ERROR), accept, the names, shutdown, and sending
// and receiving with addresses. Each Unix socket call starts by asking
// inetFD whether the descriptor is an inet socket and comes here if so.
// Docs: POSIX_COVERAGE.md.

import (
	"syscall"

	"gvisor.dev/gvisor/pkg/tcpip"
	"gvisor.dev/gvisor/pkg/waiter"
)

// inetFD is fd's inet socket and description, if fd is one.
func (p *Process) inetFD(fd uint64) (*inetSock, *file) {
	f := p.fds.get(fd)
	if f == nil {
		return nil, nil
	}
	s, isInet := f.f.(*inetSock)
	if !isInet {
		return nil, nil
	}
	return s, f
}

// inetSocket is socket(AF_INET or AF_INET6, type, protocol).
func (p *Process) inetSocket(a [6]uint64) int64 {
	st := p.sys.network()
	if st == nil {
		return -int64(syscall.EAFNOSUPPORT)
	}
	typ := int(a[1]) &^ (sockNonblock | sockCloexec)
	if typ != sockStream && typ != sockDgram && typ != sockRaw {
		return -int64(syscall.ESOCKTNOSUPPORT)
	}
	s, e := newInetSock(st, int(a[0]), typ, int(a[2]))
	if e < 0 {
		return e
	}
	return p.installSock(s, a[1])
}

// installSock gives a new socket a descriptor, with SOCK_NONBLOCK and
// SOCK_CLOEXEC from flags.
func (p *Process) installSock(s *inetSock, flags uint64) int64 {
	f := newFile(s, nil)
	f.flags = oRDWR
	if flags&sockNonblock != 0 {
		f.flags |= oNonblock
	}
	n, err := p.fds.add(f, 0, flags&sockCloexec != 0)
	if err != nil {
		f.unref()
		return errno(err)
	}
	return int64(n)
}

func (p *Process) inetBind(s *inetSock, va, n uint64) int64 {
	addr, unspec, e := p.inetAddr(s.family, va, n)
	switch {
	case e < 0:
		return e
	case unspec:
		return -int64(syscall.EAFNOSUPPORT)
	}
	if err := s.ep.Bind(addr); err != nil {
		return inetErrno(err)
	}
	return 0
}

func (s *inetSock) listen(backlog uint64) int64 {
	if s.typ != sockStream {
		return -int64(syscall.EOPNOTSUPP)
	}
	if err := s.ep.Listen(min(max(int(int32(backlog)), 0), somaxconn)); err != nil {
		return inetErrno(err)
	}
	return 0
}

// inetConnect starts a connection; blocking, it waits for the outcome.
func (t *Thread) inetConnect(s *inetSock, f *file, va, n uint64) int64 {
	addr, unspec, e := t.p.inetAddr(s.family, va, n)
	switch {
	case e < 0:
		return e
	case unspec && s.typ != sockStream:
		s.ep.Disconnect()
		return 0
	case unspec:
		return -int64(syscall.EAFNOSUPPORT)
	}
	err := s.ep.Connect(addr)
	switch err.(type) {
	case nil:
		return 0
	case *tcpip.ErrConnectStarted:
	case *tcpip.ErrAlreadyConnecting:
		return -int64(syscall.EALREADY)
	default:
		return inetErrno(err)
	}
	if f.flags&oNonblock != 0 {
		return -int64(syscall.EINPROGRESS)
	}
	for {
		ch := s.next()
		if s.ep.Readiness(waiter.WritableEvents|waiter.EventErr|waiter.EventHUp) != 0 {
			break
		}
		if err := waitFor(ch, true, t.cancel()); err != nil {
			return errno(err)
		}
	}
	if err := s.ep.LastError(); err != nil {
		return inetErrno(err)
	}
	return 0
}

// inetAccept takes a connection off the listener's queue.
func (t *Thread) inetAccept(s *inetSock, f *file, flags, addrVA, lenVA uint64) int64 {
	if flags&^(sockNonblock|sockCloexec) != 0 {
		return -int64(syscall.EINVAL)
	}
	var peer tcpip.FullAddress
	for {
		ch := s.next()
		ep, wq, err := s.ep.Accept(&peer)
		if err == nil {
			ns := &inetSock{family: s.family, typ: s.typ, proto: s.proto, ep: ep, wq: wq, changed: make(chan struct{})}
			ns.entry = waiter.NewFunctionEntry(allEvents, func(waiter.EventMask) { ns.wake() })
			wq.EventRegister(&ns.entry)
			fd := t.p.installSock(ns, flags)
			if fd >= 0 {
				if e := t.p.putAddr(inetSockaddr(s.family, peer), addrVA, lenVA); e < 0 {
					return e
				}
			}
			return fd
		}
		if _, wb := err.(*tcpip.ErrWouldBlock); !wb {
			return inetErrno(err)
		}
		if err := waitFor(ch, f.flags&oNonblock == 0, t.cancel()); err != nil {
			return errno(err)
		}
	}
}

// inetName writes the local (peer false) or remote address of s.
func (p *Process) inetName(s *inetSock, peer bool, va, lenVA uint64) int64 {
	get := s.ep.GetLocalAddress
	if peer {
		get = s.ep.GetRemoteAddress
	}
	a, err := get()
	if err != nil {
		return inetErrno(err)
	}
	return p.putAddr(inetSockaddr(s.family, a), va, lenVA)
}

func (s *inetSock) shutdown(how uint64) int64 {
	flags := map[uint64]tcpip.ShutdownFlags{0: tcpip.ShutdownRead, 1: tcpip.ShutdownWrite,
		2: tcpip.ShutdownRead | tcpip.ShutdownWrite}[how]
	if flags == 0 {
		return -int64(syscall.EINVAL)
	}
	if err := s.ep.Shutdown(flags); err != nil {
		return inetErrno(err)
	}
	return 0
}

// inetSend sends b, to the address at toVA if there is one (datagrams;
// a stream ignores it, as Linux's TCP does). EPIPE raises SIGPIPE unless
// MSG_NOSIGNAL.
func (t *Thread) inetSend(s *inetSock, f *file, b []byte, flags, toVA, toLen uint64) int64 {
	var opts tcpip.WriteOptions
	if toVA != 0 && s.typ != sockStream {
		to, _, e := t.p.inetAddr(s.family, toVA, toLen)
		if e < 0 {
			return e
		}
		opts.To = &to
	}
	n, err := s.send(b, opts, f.flags&oNonblock == 0 && flags&msgDontwait == 0, t.cancel())
	if err == syscall.EPIPE && flags&msgNosignal == 0 {
		t.send(sigPIPE, sigInfo{signo: sigPIPE, code: siKernel})
	}
	if err != nil {
		return errno(err)
	}
	return int64(n)
}

// inetRecv reads at most n bytes; MSG_WAITALL on a stream reads until n
// or the end. It returns the bytes, the datagram's whole length and where
// they came from.
func (t *Thread) inetRecv(s *inetSock, f *file, n, flags uint64) ([]byte, int, tcpip.FullAddress, int64) {
	b := make([]byte, min(n, maxIO))
	opts := tcpip.ReadOptions{Peek: flags&msgPeek != 0, NeedRemoteAddr: s.typ != sockStream}
	block := f.flags&oNonblock == 0 && flags&msgDontwait == 0
	got, total := 0, 0
	var from tcpip.FullAddress
	for {
		res, err := s.recv(b[got:], opts, block, t.cancel())
		if err != nil && got == 0 {
			return nil, 0, from, errno(err)
		}
		if got == 0 {
			from = res.RemoteAddr
		}
		got += res.Count
		total = max(total, res.Total)
		if err != nil || res.Count == 0 || s.typ != sockStream || flags&msgWaitall == 0 || got == len(b) {
			break
		}
	}
	if s.typ == sockStream {
		total = got
	}
	if from.Addr.Len() == 0 && s.typ == sockStream {
		from, _ = s.ep.GetRemoteAddress()
	}
	return b[:got], total, from, 0
}
