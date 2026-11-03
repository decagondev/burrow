package proc

// sockconn.go - connect and accept for Unix sockets (W14-S07), and the
// name queries. connect builds the pair at once and queues the server end
// on the listener; accept hands it out as a new descriptor. A full queue
// makes connect wait (EAGAIN when non-blocking), as Linux's does. Lock
// order: the listener's, then the client's. Docs: POSIX_COVERAGE.md.

import (
	"io/fs"
	"syscall"
)

// connect(fd, addr, len).
func sysConnectFn(t *Thread, a [6]uint64) (int64, bool) {
	p := t.p
	s, f, e := p.socket(a[0])
	if e < 0 {
		return ok(e)
	}
	name, key, e := p.sockaddr(a[1], a[2])
	switch {
	case e < 0:
		return ok(e)
	case name == "":
		return ok(-int64(syscall.EINVAL))
	case s.typ == sockDgram:
		return ok(-int64(syscall.EOPNOTSUPP))
	}
	s.mu.Lock()
	connected, listening := s.pair() != nil, s.lis != nil
	s.mu.Unlock()
	switch {
	case connected:
		return ok(-int64(syscall.EISCONN))
	case listening:
		return ok(-int64(syscall.EINVAL))
	}
	if name[0] != 0 {
		st, err := fs.Stat(p.sys.files, key)
		if err != nil {
			return ok(errno(err))
		}
		if st.Mode().Type() != fs.ModeSocket {
			return ok(-int64(syscall.ECONNREFUSED))
		}
	}
	l := p.sys.names.lookup(key)
	if l == nil {
		return ok(-int64(syscall.ECONNREFUSED))
	}
	return ok(t.connectTo(s, l, f.flags&oNonblock == 0))
}

// connectTo queues a new server end on listener l and joins s to it.
func (t *Thread) connectTo(s, l *sockEnd, block bool) int64 {
	for {
		l.mu.Lock()
		q := l.lis
		switch {
		case q == nil:
			l.mu.Unlock()
			return -int64(syscall.ECONNREFUSED)
		case l.typ != s.typ:
			l.mu.Unlock()
			return -int64(syscall.EPROTOTYPE)
		}
		if len(q.pending) <= q.backlog {
			srv := newSockEnd(s.typ, s.sys)
			s.mu.Lock()
			srv.name, srv.peerName, srv.peerPID = l.name, s.name, int32(t.p.PID)
			s.peerName, s.peerPID = l.name, q.pid
			join(s, srv)
			s.mu.Unlock()
			q.pending = append(q.pending, srv)
			q.notify()
			l.mu.Unlock()
			return 0
		}
		ch := q.changed
		l.mu.Unlock()
		if !block {
			return -int64(syscall.EAGAIN)
		}
		select {
		case <-ch:
		case <-t.cancel():
			return -int64(syscall.EINTR)
		}
	}
}

func sysAcceptFn(t *Thread, a [6]uint64) (int64, bool)  { return ok(t.accept(a[0], a[1], a[2], 0)) }
func sysAccept4Fn(t *Thread, a [6]uint64) (int64, bool) { return ok(t.accept(a[0], a[1], a[2], a[3])) }

// accept waits for a connection on listener fd and returns its descriptor;
// the client's address (usually unnamed) goes to addr.
func (t *Thread) accept(fd, addrVA, lenVA, flags uint64) int64 {
	p := t.p
	if flags&^(sockNonblock|sockCloexec) != 0 {
		return -int64(syscall.EINVAL)
	}
	l, f, e := p.socket(fd)
	if e < 0 {
		return e
	}
	var srv *sockEnd
	for srv == nil {
		l.mu.Lock()
		q := l.lis
		if q == nil {
			l.mu.Unlock()
			return -int64(syscall.EINVAL)
		}
		if len(q.pending) > 0 {
			srv, q.pending = q.pending[0], q.pending[1:]
			q.notify()
			l.mu.Unlock()
			break
		}
		ch := q.changed
		l.mu.Unlock()
		if f.flags&oNonblock != 0 {
			return -int64(syscall.EAGAIN)
		}
		select {
		case <-ch:
		case <-t.cancel():
			return -int64(syscall.EINTR)
		}
	}
	nf := newFile(srv, nil)
	nf.flags = oRDWR
	if flags&sockNonblock != 0 {
		nf.flags |= oNonblock
	}
	n, err := p.fds.add(nf, 0, flags&sockCloexec != 0)
	if err != nil {
		nf.unref()
		return errno(err)
	}
	srv.mu.Lock()
	peer := srv.peerName
	srv.mu.Unlock()
	if e := p.putSockaddr(peer, addrVA, lenVA); e < 0 {
		return e
	}
	return int64(n)
}

func sysGetsocknameFn(p *Process, a [6]uint64) (int64, bool) {
	s, _, e := p.socket(a[0])
	if e < 0 {
		return ok(e)
	}
	s.mu.Lock()
	name := s.name
	s.mu.Unlock()
	return ok(p.putSockaddr(name, a[1], a[2]))
}

func sysGetpeernameFn(p *Process, a [6]uint64) (int64, bool) {
	s, _, e := p.socket(a[0])
	if e < 0 {
		return ok(e)
	}
	if s.pair() == nil {
		return ok(-int64(syscall.ENOTCONN))
	}
	s.mu.Lock()
	name := s.peerName
	s.mu.Unlock()
	return ok(p.putSockaddr(name, a[1], a[2]))
}
