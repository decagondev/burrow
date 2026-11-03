package proc

// sock.go - Unix domain sockets (W14-S06): a connected pair of ends, each
// with a receive queue the other end writes into. Stream sockets are a
// byte stream like a pipe in each direction; datagram and seqpacket
// sockets keep message boundaries. Shutdown follows Linux's two bits per
// end: shutting down one side's sending marks the peer's receiving, and a
// close marks both of the peer's. Never holds the pair's lock while
// blocked. Names, listening and descriptor passing are W14-S07.
// Docs: docs/02-architecture/POSIX_COVERAGE.md.

import (
	"io/fs"
	"sync"
	"syscall"
	"time"
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

// sockPair is what the two ends share: one lock, one change channel.
type sockPair struct {
	mu      sync.Mutex
	typ     int
	end     [2]*sockEnd
	changed chan struct{} // closed and replaced on every change
}

// sockEnd is one end, an fs.File so a descriptor can hold it.
type sockEnd struct {
	pair   *sockPair
	side   int      // 0 or 1; the peer is side ^ 1
	queue  [][]byte // received, not yet read: messages (stream: chunks)
	queued int      // bytes in queue
	shut   int      // shutRcv, shutSnd
	closed bool
	err    syscall.Errno // pending error (SO_ERROR), reported once
	sndbuf int
	rcvbuf int
}

func newSockPair(typ int) (*sockEnd, *sockEnd) {
	sp := &sockPair{typ: typ, changed: make(chan struct{})}
	for i := range sp.end {
		sp.end[i] = &sockEnd{pair: sp, side: i, sndbuf: sockBufDefault, rcvbuf: sockBufDefault}
	}
	return sp.end[0], sp.end[1]
}

func (e *sockEnd) peer() *sockEnd { return e.pair.end[e.side^1] }

// notify wakes every waiter (pair.mu held).
func (sp *sockPair) notify() {
	close(sp.changed)
	sp.changed = make(chan struct{})
}

func (e *sockEnd) stream() bool { return e.pair.typ == sockStream }

// recv takes up to len(b) bytes (peek: leaves them queued). A datagram or
// seqpacket read takes one message; full is its whole length (more than n
// when b was too short; the rest is gone). With
// nothing queued it waits, unless block is false (EAGAIN); after the
// peer's sending side is shut it returns 0. waitAll keeps a stream read
// going until b is full.
func (e *sockEnd) recv(b []byte, peek, block, waitAll bool, cancel <-chan struct{}) (n, full int, err error) {
	sp := e.pair
	for {
		sp.mu.Lock()
		if e.err != 0 && n == 0 {
			err, e.err = e.err, 0
			sp.mu.Unlock()
			return 0, 0, err
		}
		if len(e.queue) > 0 && len(b) > n {
			k, whole := e.take(b[n:], peek)
			n, full = n+k, n+whole
			sp.notify()
			if !e.stream() || !waitAll || n == len(b) || peek {
				sp.mu.Unlock()
				return n, full, nil
			}
		}
		if len(e.queue) == 0 && e.shut&shutRcv != 0 || len(b) == 0 || n > 0 && !waitAll {
			sp.mu.Unlock()
			return n, n, nil
		}
		ch := sp.changed
		sp.mu.Unlock()
		if !block {
			if n > 0 {
				return n, n, nil
			}
			return 0, 0, syscall.EAGAIN
		}
		select {
		case <-ch:
		case <-cancel:
			if n > 0 {
				return n, n, nil
			}
			return 0, 0, syscall.EINTR
		}
	}
}

// take copies from the queue into b (pair.mu held): the bytes copied and
// the length of what they came from (a whole message; for a stream, the same).
func (e *sockEnd) take(b []byte, peek bool) (int, int) {
	if !e.stream() {
		m := e.queue[0]
		n := copy(b, m)
		if !peek {
			e.queue, e.queued = e.queue[1:], e.queued-len(m)
		}
		return n, len(m)
	}
	n := 0
	for i := 0; i < len(e.queue) && n < len(b); i++ {
		n += copy(b[n:], e.queue[i])
	}
	if peek {
		return n, n
	}
	e.queued -= n
	for left := n; left > 0; {
		if k := len(e.queue[0]); k <= left {
			e.queue, left = e.queue[1:], left-k
		} else {
			e.queue[0], left = e.queue[0][left:], 0
		}
	}
	return n, n
}

// send queues b at the peer: a stream writes all of it, waiting for room
// (a part, if a wait is cut short); a datagram goes whole or not at all.
func (e *sockEnd) send(b []byte, block bool, cancel <-chan struct{}) (int, error) {
	sp := e.pair
	if e.stream() && len(b) == 0 {
		return 0, nil
	}
	if !e.stream() && len(b) > e.sndbuf-32 {
		return 0, syscall.EMSGSIZE
	}
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
			p.queue = append(p.queue, append([]byte(nil), b[done:done+n]...))
			p.queued += n
			done += n
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

// shutdown(how): SHUT_RD 0, SHUT_WR 1, SHUT_RDWR 2.
func (e *sockEnd) shutdown(how int) {
	sp := e.pair
	sp.mu.Lock()
	defer sp.mu.Unlock()
	p := e.peer()
	if how != 1 { // reading
		e.shut |= shutRcv
		p.shut |= shutSnd
	}
	if how != 0 { // writing
		e.shut |= shutSnd
		p.shut |= shutRcv
	}
	sp.notify()
}

func (e *sockEnd) ReadCancel(b []byte, cancel <-chan struct{}) (int, error) {
	n, _, err := e.recv(b, false, true, false, cancel)
	return n, err
}

func (e *sockEnd) WriteCancel(b []byte, cancel <-chan struct{}) (int, error) {
	return e.send(b, true, cancel)
}

func (e *sockEnd) Read(b []byte) (int, error)  { return e.ReadCancel(b, nil) }
func (e *sockEnd) Write(b []byte) (int, error) { return e.WriteCancel(b, nil) }

// Close: a stream or seqpacket peer sees both its sides shut (end of file,
// EPIPE), and ECONNRESET if this end still held unread data.
func (e *sockEnd) Close() error {
	sp := e.pair
	sp.mu.Lock()
	defer sp.mu.Unlock()
	if e.closed {
		return nil
	}
	e.closed = true
	if p := e.peer(); sp.typ != sockDgram {
		p.shut = shutRcv | shutSnd
		if e.queued > 0 {
			p.err = syscall.ECONNRESET
		}
	}
	e.queue, e.queued = nil, 0
	sp.notify()
	return nil
}

// Poll: readable with data, at end of file or with an error; writable
// when a write would not wait; RDHUP once the peer stopped sending, HUP
// once both sides are shut (Linux's unix_poll).
func (e *sockEnd) Poll() (int16, <-chan struct{}) {
	sp := e.pair
	sp.mu.Lock()
	defer sp.mu.Unlock()
	var rev int16
	if len(e.queue) > 0 || e.shut&shutRcv != 0 {
		rev |= pollIn
	}
	if e.shut&shutRcv != 0 {
		rev |= pollRdhup
	}
	if e.shut == shutRcv|shutSnd {
		rev |= pollHup
	}
	if e.err != 0 {
		rev |= pollErr
	}
	if e.shut&shutSnd != 0 || e.peer().closed || e.sndbuf-e.peer().queued > 0 && e.stream() ||
		!e.stream() && e.peer().queued < e.sndbuf {
		rev |= pollOut
	}
	return rev, sp.changed
}

func (e *sockEnd) Stat() (fs.FileInfo, error) { return sockInfo{}, nil }

type sockInfo struct{}

func (sockInfo) Name() string       { return "socket" }
func (sockInfo) Size() int64        { return 0 }
func (sockInfo) Mode() fs.FileMode  { return fs.ModeSocket | 0o777 }
func (sockInfo) ModTime() time.Time { return time.Time{} }
func (sockInfo) IsDir() bool        { return false }
func (sockInfo) Sys() any           { return nil }
