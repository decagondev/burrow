package proc

// sockdgram.go - named datagram Unix sockets (W23-S06, E14.F08): an
// unconnected datagram socket has a mailbox, a receive queue with the same
// lock-and-notify discipline as a pair's, that any socket may send to by
// name (sendto, or a default peer set by connect). Each message keeps the
// sender's bound name, which recvfrom and recvmsg report; descriptors and
// credentials ride along as on a pair. A full mailbox makes the sender wait
// (or EAGAIN); a name with no socket is ECONNREFUSED. Never holds a mailbox
// lock while blocked. Docs: docs/02-architecture/POSIX_COVERAGE.md.

import (
	"io/fs"
	"syscall"
)

// mailbox is e's own receive queue, made on first use.
func (e *sockEnd) mailbox() *sockPair {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.box == nil {
		e.box = &sockPair{end: [2]*sockEnd{e, nil}, changed: make(chan struct{})}
	}
	return e.box
}

// inbox is where e's messages wait: its pair once connected, else its mailbox
// (a datagram socket's).
func (e *sockEnd) inbox() *sockPair {
	if sp := e.pair(); sp != nil {
		return sp
	}
	return e.mailbox()
}

// unpairedDgram says whether e is a datagram socket with a mailbox's ways.
func (e *sockEnd) unpairedDgram() bool { return e.typ == sockDgram && e.pair() == nil }

// dgramTarget is the socket a datagram from s goes to: at the address at
// toVA, or s's default peer (connect); -errno if there is none.
func (p *Process) dgramTarget(s *sockEnd, toVA, toLen uint64) (*sockEnd, int64) {
	key := ""
	if toVA != 0 {
		name, k, e := p.sockaddr(toVA, toLen, true)
		if e < 0 {
			return nil, e
		}
		if name == "" {
			return nil, -int64(syscall.EINVAL)
		}
		if name[0] != 0 {
			if st, err := fs.Stat(p.sys.files, k); err != nil {
				return nil, errno(err)
			} else if st.Mode().Type() != fs.ModeSocket {
				return nil, -int64(syscall.ECONNREFUSED)
			}
		}
		key = k
	} else {
		s.mu.Lock()
		key = s.dest
		s.mu.Unlock()
		if key == "" {
			return nil, -int64(syscall.ENOTCONN)
		}
	}
	return s.target(key)
}

// target is the datagram socket bound to key, or -errno.
func (s *sockEnd) target(key string) (*sockEnd, int64) {
	dst := s.sys.names.lookup(key)
	switch {
	case dst == nil:
		return nil, -int64(syscall.ECONNREFUSED)
	case dst.typ != sockDgram:
		return nil, -int64(syscall.EPROTOTYPE)
	case dst.pair() != nil:
		return nil, -int64(syscall.EPERM) // connected to another: not ours to write to
	}
	return dst, 0
}

// sendDefault is write(2) on an unconnected datagram socket: to its
// default peer (connect), ENOTCONN without one.
func (s *sockEnd) sendDefault(b []byte, cred peerCred, cancel <-chan struct{}) (int, error) {
	s.mu.Lock()
	key := s.dest
	s.mu.Unlock()
	if key == "" {
		return 0, syscall.ENOTCONN
	}
	dst, e := s.target(key)
	if e < 0 {
		return 0, syscall.Errno(-e)
	}
	return s.sendTo(dst, b, nil, cred, true, cancel)
}

// sendTo queues one datagram from e in dst's mailbox. It owns fds.
func (e *sockEnd) sendTo(dst *sockEnd, b []byte, fds []*file, cred peerCred, block bool, cancel <-chan struct{}) (int, error) {
	defer func() { unrefAll(fds) }()
	e.mu.Lock()
	cred.name = e.name
	limit := e.sndbuf
	e.mu.Unlock()
	if len(b) > limit-32 {
		return 0, syscall.EMSGSIZE
	}
	dst.mu.Lock()
	room := dst.rcvbuf
	dst.mu.Unlock()
	box := dst.mailbox()
	for {
		box.mu.Lock()
		if dst.closed {
			box.mu.Unlock()
			return 0, syscall.ECONNREFUSED
		}
		if room-dst.queued >= len(b) {
			dst.queue = append(dst.queue, chunk{append([]byte(nil), b...), fds, cred})
			dst.queued += len(b)
			fds = nil
			box.notify()
			box.mu.Unlock()
			return len(b), nil
		}
		ch := box.changed
		box.mu.Unlock()
		if !block {
			return 0, syscall.EAGAIN
		}
		select {
		case <-ch:
		case <-cancel:
			return 0, syscall.EINTR
		}
	}
}

// connectDgram sets s's default peer (name "" forgets it, as AF_UNSPEC does).
func (p *Process) connectDgram(s *sockEnd, name, key string) int64 {
	if s.pair() != nil {
		return -int64(syscall.EISCONN)
	}
	if name != "" {
		dst := p.sys.names.lookup(key)
		switch {
		case dst == nil:
			return -int64(syscall.ECONNREFUSED)
		case dst.typ != sockDgram:
			return -int64(syscall.EPROTOTYPE)
		}
	}
	s.mu.Lock()
	s.dest, s.peerName = key, name
	s.mu.Unlock()
	return 0
}

// closeMailbox drops what waited in e's mailbox; later senders are refused.
func (e *sockEnd) closeMailbox() {
	e.mu.Lock()
	box := e.box
	e.mu.Unlock()
	if box == nil {
		return
	}
	box.mu.Lock()
	defer box.mu.Unlock()
	e.closed = true
	for _, c := range e.queue {
		unrefAll(c.fds)
	}
	e.queue, e.queued = nil, 0
	box.notify()
}

// pollMailbox: readable with a datagram waiting; writable always (a send
// waits on its target's mailbox, not on this socket).
func (e *sockEnd) pollMailbox() (int16, <-chan struct{}) {
	box := e.mailbox()
	box.mu.Lock()
	defer box.mu.Unlock()
	rev := int16(pollOut)
	if len(e.queue) > 0 {
		rev |= pollIn
	}
	return rev, box.changed
}
