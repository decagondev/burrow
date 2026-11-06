package proc

// sock.go - Unix domain sockets (W14-S06, S07): an end is an fs.File; once
// connected (socketpair, or connect and accept, socknames.go) it shares a
// sockPair with its peer and has a receive queue the peer writes into.
// Stream sockets are a byte stream like a pipe in each direction; datagram
// and seqpacket sockets keep message boundaries. Queued chunks carry the
// descriptors passed with them (SCM_RIGHTS) and the sender's pid.
// Shutdown follows Linux's two bits per end: shutting down one side's
// sending marks the peer's receiving, and a close marks both of the
// peer's. Never holds a lock while blocked; lock order is e.mu, then the
// pair's. Docs: docs/02-architecture/POSIX_COVERAGE.md.

import (
	"sync"
	"sync/atomic"
	"syscall"
)

const (
	sockStream    = 1
	sockDgram     = 2
	sockSeqpacket = 5

	sockBufDefault = 212992 // Linux's wmem_default and rmem_default
	sockBufMax     = 212992 // rmem_max and wmem_max
	sockBufMin     = 2304   // SOCK_MIN_RCVBUF

	shutRcv = 1
	shutSnd = 2

	pollRdhup = 0x2000
)

// sockPair is what two connected ends share: one lock, one change channel.
type sockPair struct {
	mu      sync.Mutex
	end     [2]*sockEnd
	changed chan struct{} // closed and replaced on every change
}

// chunk is one write: its bytes, the descriptors passed with it (given
// out with its first byte), and the sender (W19-S03: its pid and ids).
type chunk struct {
	data []byte
	fds  []*file
	cred peerCred
}

// sockEnd is one socket. Before it is connected only e.mu guards it.
type sockEnd struct {
	sp  atomic.Pointer[sockPair] // nil until connected
	typ int
	sys *System // the name table (socknames.go)

	mu       sync.Mutex
	name     string // bound: a path, "\x00..." (abstract) or "" (unnamed)
	peerName string
	peerCred peerCred // SO_PEERCRED
	passcred bool
	lis      *listenQ  // listening
	box      *sockPair // an unconnected datagram socket's mailbox (sockdgram.go, W23-S06)
	dest     string    // its default peer's table key (connect)

	// Guarded by the pair's lock once connected (by the mailbox's, W23-S06).
	side   int
	queue  []chunk
	queued int
	shut   int // shutRcv, shutSnd
	closed bool
	err    syscall.Errno // pending error (SO_ERROR), reported once
	sndbuf int
	rcvbuf int
}

func newSockEnd(typ int, sys *System) *sockEnd {
	return &sockEnd{typ: typ, sys: sys, sndbuf: sockBufDefault, rcvbuf: sockBufDefault}
}

// join connects a and b (neither connected yet) as one pair.
func join(a, b *sockEnd) {
	sp := &sockPair{end: [2]*sockEnd{a, b}, changed: make(chan struct{})}
	a.side, b.side = 0, 1
	a.sp.Store(sp)
	b.sp.Store(sp)
}

func newSockPair(typ int, sys *System, cred peerCred) (*sockEnd, *sockEnd) {
	a, b := newSockEnd(typ, sys), newSockEnd(typ, sys)
	a.peerCred, b.peerCred = cred, cred
	join(a, b)
	return a, b
}

func (e *sockEnd) pair() *sockPair { return e.sp.Load() }
func (e *sockEnd) peer() *sockEnd  { return e.pair().end[e.side^1] }
func (e *sockEnd) stream() bool    { return e.typ == sockStream }

// notify wakes every waiter (sp.mu held).
func (sp *sockPair) notify() {
	close(sp.changed)
	sp.changed = make(chan struct{})
}

func unrefAll(fds []*file) {
	for _, f := range fds {
		f.unref()
	}
}

// recv takes up to len(b) bytes (peek: leaves them queued). A datagram or
// seqpacket read takes one message; full is its whole length (more than n
// when b was too short; the rest is gone). fds are the descriptors that
// came with what was read (the caller owns them); pid is the sender. With
// nothing queued it waits, unless block is false (EAGAIN); after the
// peer's sending side is shut it returns 0. waitAll keeps a stream read
// going until b is full.
func (e *sockEnd) recv(b []byte, peek, block, waitAll bool, cancel <-chan struct{}) (n, full int, fds []*file, cred peerCred, err error) {
	sp := e.inbox()
	for {
		sp.mu.Lock()
		if e.err != 0 && n == 0 {
			err, e.err = e.err, 0
			sp.mu.Unlock()
			return 0, 0, nil, peerCred{}, err
		}
		if len(e.queue) > 0 && len(b) > n && (n == 0 || e.queue[0].fds == nil) {
			if n == 0 {
				cred = e.queue[0].cred
			}
			k, whole, got := e.take(b[n:], peek)
			n, full, fds = n+k, n+whole, append(fds, got...)
			sp.notify()
			if !e.stream() || !waitAll || n == len(b) || peek || got != nil {
				sp.mu.Unlock()
				return n, full, fds, cred, nil
			}
		}
		// Nothing more to take now. A waitAll read held up by a chunk that
		// carries descriptors returns what it has.
		if len(e.queue) == 0 && e.shut&shutRcv != 0 || len(b) == 0 || n > 0 && (!waitAll || len(e.queue) > 0) {
			sp.mu.Unlock()
			return n, n, fds, cred, nil
		}
		ch := sp.changed
		sp.mu.Unlock()
		if !block {
			if n > 0 {
				return n, n, fds, cred, nil
			}
			return 0, 0, nil, peerCred{}, syscall.EAGAIN
		}
		select {
		case <-ch:
		case <-cancel:
			if n > 0 {
				return n, n, fds, cred, nil
			}
			return 0, 0, nil, peerCred{}, syscall.EINTR
		}
	}
}

// take copies from the queue into b (sp.mu held): the bytes copied, the
// length of what they came from (a whole message; for a stream, the
// same), and the descriptors that rode on it. A stream read stops before
// a later chunk that carries descriptors, as Linux's does.
func (e *sockEnd) take(b []byte, peek bool) (int, int, []*file) {
	if !e.stream() {
		m := e.queue[0]
		n := copy(b, m.data)
		if peek {
			return n, len(m.data), nil
		}
		e.queue, e.queued = e.queue[1:], e.queued-len(m.data)
		return n, len(m.data), m.fds
	}
	n := 0
	for i := 0; i < len(e.queue) && n < len(b); i++ {
		if i > 0 && e.queue[i].fds != nil {
			break
		}
		n += copy(b[n:], e.queue[i].data)
	}
	if peek {
		return n, n, nil
	}
	fds := e.queue[0].fds
	e.queue[0].fds = nil
	e.queued -= n
	for left := n; left > 0; {
		if k := len(e.queue[0].data); k <= left {
			e.queue, left = e.queue[1:], left-k
		} else {
			e.queue[0].data, left = e.queue[0].data[left:], 0
		}
	}
	return n, n, fds
}

// send queues b, with fds on its first chunk, at the peer: a stream writes
// all of it, waiting for room (a part, if a wait is cut short); a datagram
// goes whole or not at all. send owns fds: what it does not queue it drops.
func (e *sockEnd) send(b []byte, fds []*file, cred peerCred, block bool, cancel <-chan struct{}) (int, error) {
	defer func() { unrefAll(fds) }()
	if e.stream() && len(b) == 0 {
		return 0, nil
	}
	if !e.stream() && len(b) > e.sndbuf-32 {
		return 0, syscall.EMSGSIZE
	}
	sp := e.pair()
	done := 0
	for done < len(b) || len(b) == 0 && done == 0 {
		sp.mu.Lock()
		p := e.peer()
		switch {
		case e.shut&shutSnd != 0:
			sp.mu.Unlock()
			return partial(done, syscall.EPIPE)
		case p.closed: // a datagram pair's peer is gone (a stream's close shuts us)
			sp.mu.Unlock()
			return partial(done, syscall.ECONNREFUSED)
		}
		room := e.sndbuf - p.queued
		if e.stream() && room > 0 || !e.stream() && room >= len(b) {
			n := len(b) - done
			if e.stream() {
				n = min(room, n)
			}
			p.queue = append(p.queue, chunk{append([]byte(nil), b[done:done+n]...), fds, cred})
			p.queued += n
			done += n
			fds = nil
			sp.notify()
			sp.mu.Unlock()
			if len(b) == 0 {
				return 0, nil
			}
			continue
		}
		ch := sp.changed
		sp.mu.Unlock()
		if !block {
			return partial(done, syscall.EAGAIN)
		}
		select {
		case <-ch:
		case <-cancel:
			return partial(done, syscall.EINTR)
		}
	}
	return done, nil
}

func partial(done int, err error) (int, error) {
	if done > 0 {
		return done, nil
	}
	return 0, err
}
