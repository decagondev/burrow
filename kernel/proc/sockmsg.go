package proc

// sockmsg.go - sendmsg and recvmsg (struct msghdr; no control messages
// until W14-S07), the SOL_SOCKET options and FIONREAD for sockets
// (W14-S06). Never keeps a msghdr's pointers past the call.
// Docs: docs/02-architecture/POSIX_COVERAGE.md.

import (
	"encoding/binary"
	"syscall"
)

// msghdr: name @0, namelen u32 @8, iov @16, iovlen @24, control @32,
// controllen @40, flags int @48.
type msghdr struct{ name, namelen, iov, iovlen, control, controllen uint64 }

func (p *Process) readMsghdr(va uint64) (msghdr, []byte, int64) {
	b := make([]byte, msghdrBytes)
	if err := p.mem.CopyIn(va, b); err != nil {
		return msghdr{}, nil, errno(err)
	}
	le := binary.LittleEndian
	return msghdr{le.Uint64(b[0:]), uint64(le.Uint32(b[8:])), le.Uint64(b[16:]), le.Uint64(b[24:]),
		le.Uint64(b[32:]), le.Uint64(b[40:])}, b, 0
}

// iovecs reads n struct iovec {base, len} at va.
func (p *Process) iovecs(va, n uint64) ([][2]uint64, int64) {
	if n > 1024 {
		return nil, -int64(syscall.EMSGSIZE)
	}
	raw := make([]byte, 16*n)
	if err := p.mem.CopyIn(va, raw); err != nil {
		return nil, errno(err)
	}
	out := make([][2]uint64, n)
	total := uint64(0)
	for i := range out {
		out[i] = [2]uint64{binary.LittleEndian.Uint64(raw[16*i:]), binary.LittleEndian.Uint64(raw[16*i+8:])}
		if total += out[i][1]; total > maxIO {
			return nil, -int64(syscall.EINVAL)
		}
	}
	return out, 0
}

func sysSendmsgFn(t *Thread, a [6]uint64) (int64, bool) {
	p := t.p
	h, _, e := p.readMsghdr(a[1])
	if e < 0 {
		return ok(e)
	}
	if h.controllen != 0 {
		return ok(-int64(syscall.EOPNOTSUPP)) // SCM_RIGHTS and SCM_CREDENTIALS are W14-S07
	}
	iov, e := p.iovecs(h.iov, h.iovlen)
	if e < 0 {
		return ok(e)
	}
	var all []byte
	for _, v := range iov {
		b := make([]byte, v[1])
		if err := p.mem.CopyIn(v[0], b); err != nil {
			return ok(errno(err))
		}
		all = append(all, b...)
	}
	return ok(t.sendBytes(a[0], all, a[2], h.name))
}

func sysRecvmsgFn(t *Thread, a [6]uint64) (int64, bool) {
	p := t.p
	h, raw, e := p.readMsghdr(a[1])
	if e < 0 {
		return ok(e)
	}
	iov, e := p.iovecs(h.iov, h.iovlen)
	if e < 0 {
		return ok(e)
	}
	total := uint64(0)
	for _, v := range iov {
		total += v[1]
	}
	b, full, e := t.recvBytes(a[0], total, a[2])
	if e < 0 {
		return ok(e)
	}
	n := len(b)
	for _, v := range iov {
		k := min(uint64(len(b)), v[1])
		if err := p.mem.CopyOut(v[0], b[:k]); err != nil {
			return ok(errno(err))
		}
		b = b[k:]
	}
	le := binary.LittleEndian
	if h.name != 0 {
		if int32(h.namelen) >= 2 {
			if err := p.mem.CopyOut(h.name, []byte{afUnix, 0}); err != nil {
				return ok(errno(err))
			}
		}
		le.PutUint32(raw[8:], 2)
	}
	le.PutUint64(raw[40:], 0) // no control messages
	flags := uint32(0)
	if full > n {
		flags = msgTrunc
	}
	le.PutUint32(raw[48:], flags)
	if err := p.mem.CopyOut(a[1], raw); err != nil {
		return ok(errno(err))
	}
	if a[2]&msgTrunc != 0 {
		return ok(int64(full))
	}
	return ok(int64(n))
}

// getsockopt(fd, level, name, value, int *len).
func sysGetsockoptFn(p *Process, a [6]uint64) (int64, bool) {
	s, _, e := p.socket(a[0])
	if e < 0 {
		return ok(e)
	}
	var l [4]byte
	if err := p.mem.CopyIn(a[4], l[:]); err != nil {
		return ok(errno(err))
	}
	n := int32(binary.LittleEndian.Uint32(l[:]))
	if n < 0 {
		return ok(-int64(syscall.EINVAL))
	}
	v, e := s.option(a[1], a[2])
	if e < 0 {
		return ok(e)
	}
	var out [4]byte
	binary.LittleEndian.PutUint32(out[:], uint32(v))
	if err := p.mem.CopyOut(a[3], out[:min(n, 4)]); err != nil {
		return ok(errno(err))
	}
	binary.LittleEndian.PutUint32(l[:], uint32(min(n, 4)))
	return ok(errno(p.mem.CopyOut(a[4], l[:])))
}

func (e *sockEnd) option(level, name uint64) (int32, int64) {
	sp := e.pair
	sp.mu.Lock()
	defer sp.mu.Unlock()
	if level != solSocket {
		return 0, -int64(syscall.ENOPROTOOPT)
	}
	switch name {
	case soType:
		return int32(sp.typ), 0
	case soError:
		v := int32(e.err)
		e.err = 0
		return v, 0
	case soSndbuf:
		return int32(e.sndbuf), 0
	case soRcvbuf:
		return int32(e.rcvbuf), 0
	}
	return 0, -int64(syscall.ENOPROTOOPT)
}

// setsockopt(fd, level, name, value, len): the buffer sizes, kept as Linux
// keeps them (doubled, within its limits).
func sysSetsockoptFn(p *Process, a [6]uint64) (int64, bool) {
	s, _, e := p.socket(a[0])
	switch {
	case e < 0:
		return ok(e)
	case a[1] != solSocket || a[2] != soSndbuf && a[2] != soRcvbuf:
		return ok(-int64(syscall.ENOPROTOOPT))
	case a[4] < 4:
		return ok(-int64(syscall.EINVAL))
	}
	var b [4]byte
	if err := p.mem.CopyIn(a[3], b[:]); err != nil {
		return ok(errno(err))
	}
	v := int(min(binary.LittleEndian.Uint32(b[:]), sockBufMax))
	v = max(2*v, sockBufMin)
	s.pair.mu.Lock()
	if a[2] == soSndbuf {
		s.sndbuf = v
	} else {
		s.rcvbuf = v
	}
	s.pair.notify()
	s.pair.mu.Unlock()
	return ok(0)
}

// Ioctl: FIONREAD is the bytes a read would get (a stream's queue, the
// next datagram).
func (e *sockEnd) Ioctl(req, arg uint64, _, out func(va uint64, b []byte) error) (int64, error) {
	if req != fionread {
		return 0, syscall.ENOTTY
	}
	e.pair.mu.Lock()
	n := e.queued
	if !e.stream() && len(e.queue) > 0 {
		n = len(e.queue[0])
	}
	e.pair.mu.Unlock()
	var b [4]byte
	binary.LittleEndian.PutUint32(b[:], uint32(n))
	return 0, out(arg, b[:])
}
