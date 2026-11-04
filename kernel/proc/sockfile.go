package proc

// sockfile.go - a socket as a file (W14-S06, S07): read and write (which
// drop any descriptors that came along, as Linux's read does), readiness
// for poll/select/epoll, fstat (S_IFSOCK), FIONREAD and the SOL_SOCKET
// options. Before it is connected a socket answers as Linux's does: a read
// is EINVAL, a write ENOTCONN, a listener is readable with a connection
// waiting. Docs: docs/02-architecture/POSIX_COVERAGE.md.

import (
	"encoding/binary"
	"io/fs"
	"syscall"
	"time"
)

func (e *sockEnd) ReadCancel(b []byte, cancel <-chan struct{}) (int, error) {
	if e.pair() == nil {
		return 0, syscall.EINVAL
	}
	n, _, fds, _, err := e.recv(b, false, true, false, cancel)
	unrefAll(fds)
	return n, err
}

func (e *sockEnd) WriteCancel(b []byte, cancel <-chan struct{}) (int, error) {
	return e.sendFrom(b, peerCred{}, cancel)
}

// sendFrom is write(2) by the process cred names (write passes it: SCM_CREDENTIALS).
func (e *sockEnd) sendFrom(b []byte, cred peerCred, cancel <-chan struct{}) (int, error) {
	if e.pair() == nil {
		return 0, syscall.ENOTCONN
	}
	return e.send(b, nil, cred, true, cancel)
}

// shutdown(how): SHUT_RD 0, SHUT_WR 1, SHUT_RDWR 2.
func (e *sockEnd) shutdown(how int) error {
	sp := e.pair()
	if sp == nil {
		return syscall.ENOTCONN
	}
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
	return nil
}

// Close: a stream or seqpacket peer sees both its sides shut (end of file,
// EPIPE), and ECONNRESET if this end still held unread data. A listener
// closes the connections nobody accepted; a name goes from the table.
func (e *sockEnd) Close() error {
	e.unlisten()
	e.sys.names.drop(e)
	sp := e.pair()
	if sp == nil {
		return nil
	}
	sp.mu.Lock()
	defer sp.mu.Unlock()
	if e.closed {
		return nil
	}
	e.closed = true
	if p := e.peer(); e.typ != sockDgram {
		p.shut = shutRcv | shutSnd
		if e.queued > 0 {
			p.err = syscall.ECONNRESET
		}
	}
	for _, c := range e.queue {
		unrefAll(c.fds)
	}
	e.queue, e.queued = nil, 0
	sp.notify()
	return nil
}

func (e *sockEnd) Read(b []byte) (int, error)  { return e.ReadCancel(b, nil) }
func (e *sockEnd) Write(b []byte) (int, error) { return e.WriteCancel(b, nil) }

// Poll: readable with data, at end of file or with an error; writable
// when a write would not wait; RDHUP once the peer stopped sending, HUP
// once both sides are shut (Linux's unix_poll). A listener is readable
// with a connection to accept; an unconnected stream socket is HUP.
func (e *sockEnd) Poll() (int16, <-chan struct{}) {
	sp := e.pair()
	if sp == nil {
		return e.pollUnconnected()
	}
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
	p := e.peer()
	if e.shut&shutSnd != 0 || p.closed || e.stream() && e.sndbuf > p.queued || !e.stream() && p.queued < e.sndbuf {
		rev |= pollOut
	}
	return rev, sp.changed
}

func (e *sockEnd) pollUnconnected() (int16, <-chan struct{}) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if q := e.lis; q != nil {
		if len(q.pending) > 0 {
			return pollIn, q.changed
		}
		return 0, q.changed
	}
	if e.typ == sockDgram {
		return pollOut, nil
	}
	return pollOut | pollHup, nil
}

func (e *sockEnd) Stat() (fs.FileInfo, error) { return sockInfo{}, nil }

type sockInfo struct{}

func (sockInfo) Name() string       { return "socket" }
func (sockInfo) Size() int64        { return 0 }
func (sockInfo) Mode() fs.FileMode  { return fs.ModeSocket | 0o777 }
func (sockInfo) ModTime() time.Time { return time.Time{} }
func (sockInfo) IsDir() bool        { return false }
func (sockInfo) Sys() any           { return nil }

// Ioctl: FIONREAD is the bytes a read would get (a stream's queue, the
// next datagram).
func (e *sockEnd) Ioctl(req, arg uint64, _, out func(va uint64, b []byte) error) (int64, error) {
	if req != fionread {
		return 0, syscall.ENOTTY
	}
	n := 0
	if sp := e.pair(); sp != nil {
		sp.mu.Lock()
		n = e.queued
		if !e.stream() && len(e.queue) > 0 {
			n = len(e.queue[0].data)
		}
		sp.mu.Unlock()
	}
	var b [4]byte
	binary.LittleEndian.PutUint32(b[:], uint32(n))
	return 0, out(arg, b[:])
}

// lockAll takes e.mu and, once connected, the pair's lock; the returned
// function releases both. Options are read under either and set under both.
func (e *sockEnd) lockAll() func() {
	e.mu.Lock()
	sp := e.pair()
	if sp == nil {
		return e.mu.Unlock
	}
	sp.mu.Lock()
	return func() { sp.mu.Unlock(); e.mu.Unlock() }
}

// option is getsockopt(SOL_SOCKET, name) as an int, or ENOPROTOOPT.
func (e *sockEnd) option(level, name uint64) ([]byte, int64) {
	if level != solSocket {
		return nil, -int64(syscall.ENOPROTOOPT)
	}
	unlock := e.lockAll()
	defer unlock()
	var v int32
	switch name {
	case soType:
		v = int32(e.typ)
	case soError:
		v, e.err = int32(e.err), 0
	case soSndbuf:
		v = int32(e.sndbuf)
	case soRcvbuf:
		v = int32(e.rcvbuf)
	case soPasscred:
		if e.passcred {
			v = 1
		}
	case soAcceptconn:
		if e.lis != nil {
			v = 1
		}
	case soPeercred:
		return ucred(e.peerCred), 0
	default:
		return nil, -int64(syscall.ENOPROTOOPT)
	}
	var b [4]byte
	binary.LittleEndian.PutUint32(b[:], uint32(v))
	return b[:], 0
}

// ucred is struct ucred {pid, uid, gid}.
func ucred(c peerCred) []byte {
	b := make([]byte, 12)
	binary.LittleEndian.PutUint32(b, uint32(c.pid))
	binary.LittleEndian.PutUint32(b[4:], c.uid)
	binary.LittleEndian.PutUint32(b[8:], c.gid)
	return b
}

// setOption is setsockopt(SOL_SOCKET, name, v): the buffer sizes (kept as
// Linux keeps them: doubled, within its limits) and SO_PASSCRED.
func (e *sockEnd) setOption(level, name uint64, v uint32) int64 {
	if level != solSocket {
		return -int64(syscall.ENOPROTOOPT)
	}
	unlock := e.lockAll()
	defer unlock()
	size := max(2*int(min(v, sockBufMax)), sockBufMin)
	switch name {
	case soSndbuf:
		e.sndbuf = size
	case soRcvbuf:
		e.rcvbuf = size
	case soPasscred:
		e.passcred = v != 0
	default:
		return -int64(syscall.ENOPROTOOPT)
	}
	if sp := e.pair(); sp != nil {
		sp.notify()
	}
	return 0
}
