package proc

// sockmsg.go - sendmsg and recvmsg (W14-S06) with control messages
// (W14-S07): SCM_RIGHTS passes descriptors (the receiver gets new ones for
// the same open file descriptions; MSG_CTRUNC when its buffer is short,
// the ones that did not fit are closed), SCM_CREDENTIALS is accepted from
// a sender and given to a receiver that set SO_PASSCRED. Never keeps a
// msghdr's pointers past the call. Docs: POSIX_COVERAGE.md.

import (
	"encoding/binary"
	"syscall"
)

const (
	scmRights      = 1
	scmCredentials = 2
	scmMaxFD       = 253 // SCM_MAX_FD
	cmsgHdrBytes   = 16  // struct cmsghdr {size_t len; int level, type;}
	ucredBytes     = 12
	msgCtrunc      = 0x8
	msgCmsgCloexec = 0x40000000
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

func cmsgAlign(n uint64) uint64 { return (n + 7) &^ 7 }

// controlFDs reads a sender's control messages: the descriptors of its
// SCM_RIGHTS (referenced; the caller owns them), or -errno.
func (p *Process) controlFDs(va, n uint64) ([]*file, int64) {
	if n == 0 {
		return nil, 0
	}
	if n > 4096 {
		return nil, -int64(syscall.ENOBUFS)
	}
	b := make([]byte, n)
	if err := p.mem.CopyIn(va, b); err != nil {
		return nil, errno(err)
	}
	le := binary.LittleEndian
	var fds []*file
	for o := uint64(0); o+cmsgHdrBytes <= n; {
		l, level, typ := le.Uint64(b[o:]), le.Uint32(b[o+8:]), le.Uint32(b[o+12:])
		if l < cmsgHdrBytes || l > n-o || level != solSocket {
			unrefAll(fds)
			return nil, -int64(syscall.EINVAL)
		}
		switch {
		case typ == scmRights:
			for i := o + cmsgHdrBytes; i+4 <= o+l; i += 4 {
				f := p.fds.get(uint64(le.Uint32(b[i:])))
				if f == nil || len(fds) == scmMaxFD {
					unrefAll(fds)
					if f == nil {
						return nil, -int64(syscall.EBADF)
					}
					return nil, -int64(syscall.EINVAL)
				}
				fds = append(fds, f.ref())
			}
		case typ == scmCredentials && l == cmsgHdrBytes+ucredBytes:
			// W19-S03: a claim must be the sender's own (root may claim any);
			// the receiver is given the sender's pid and effective ids
			if !p.mayClaim(b[o+cmsgHdrBytes : o+l]) {
				unrefAll(fds)
				return nil, -int64(syscall.EPERM)
			}
		default:
			unrefAll(fds)
			return nil, -int64(syscall.EINVAL)
		}
		o += cmsgAlign(l)
	}
	return fds, 0
}

func sysSendmsgFn(t *Thread, a [6]uint64) (int64, bool) {
	p := t.p
	h, _, e := p.readMsghdr(a[1])
	if e < 0 {
		return ok(e)
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
	if is, f := p.inetFD(a[0]); is != nil { // control messages are not kept
		return ok(t.inetSend(is, f, all, a[2], h.name, h.namelen))
	}
	fds, e := p.controlFDs(h.control, h.controllen)
	if e < 0 {
		return ok(e)
	}
	return ok(t.sendBytes(a[0], all, fds, a[2], h.name))
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
	if is, f := p.inetFD(a[0]); is != nil {
		return ok(t.inetRecvmsg(is, f, a, h, raw, iov, total))
	}
	r, e := t.recvBytes(a[0], total, a[2])
	if e < 0 {
		return ok(e)
	}
	b := r.b
	for _, v := range iov {
		k := min(uint64(len(b)), v[1])
		if err := p.mem.CopyOut(v[0], b[:k]); err != nil {
			unrefAll(r.fds)
			return ok(errno(err))
		}
		b = b[k:]
	}
	le := binary.LittleEndian
	flags := uint32(0)
	if r.full > len(r.b) {
		flags |= msgTrunc
	}
	ctl, ctrunc := p.controlOut(a[0], r, h.controllen, a[2]&msgCmsgCloexec != 0)
	if ctrunc {
		flags |= msgCtrunc
	}
	if err := p.mem.CopyOut(h.control, ctl); err != nil && len(ctl) > 0 {
		return ok(errno(err))
	}
	if h.name != 0 { // msg_namelen is the room in, the length out, as accept's
		if e := p.putPeerName(a[0], h.name, a[1]+8); e < 0 {
			return ok(e)
		}
	}
	le.PutUint64(raw[40:], uint64(len(ctl)))
	le.PutUint32(raw[48:], flags)
	if err := p.mem.CopyOut(a[1]+40, raw[40:]); err != nil {
		return ok(errno(err))
	}
	if a[2]&msgTrunc != 0 {
		return ok(int64(r.full))
	}
	return ok(int64(len(r.b)))
}

// controlOut builds recvmsg's control messages for r in at most room
// bytes: SCM_CREDENTIALS for a receiver with SO_PASSCRED, then SCM_RIGHTS
// with new descriptors. What does not fit is dropped (its descriptors
// closed) and ctrunc is set.
func (p *Process) controlOut(fd uint64, r received, room uint64, cloexec bool) (out []byte, ctrunc bool) {
	s, _, _ := p.socket(fd)
	s.mu.Lock()
	pass := s.passcred
	s.mu.Unlock()
	le := binary.LittleEndian
	put := func(typ uint32, data []byte) bool {
		l := cmsgHdrBytes + uint64(len(data))
		if uint64(len(out))+l > room {
			return false
		}
		h := make([]byte, cmsgHdrBytes)
		le.PutUint64(h, l)
		le.PutUint32(h[8:], solSocket)
		le.PutUint32(h[12:], typ)
		out = append(append(out, h...), data...)
		if pad := cmsgAlign(l) - l; uint64(len(out))+pad <= room {
			out = append(out, make([]byte, pad)...)
		}
		return true
	}
	if pass && !put(scmCredentials, ucred(r.cred)) {
		ctrunc = true
	}
	fit := 0
	if used := uint64(len(out)); room > used+cmsgHdrBytes {
		fit = min(int((room-used-cmsgHdrBytes)/4), len(r.fds))
	}
	var nums []byte
	for i, f := range r.fds {
		if i >= fit {
			f.unref()
			ctrunc = true
			continue
		}
		n, err := p.fds.add(f, 0, cloexec)
		if err != nil {
			f.unref()
			ctrunc = true
			continue
		}
		nums = le.AppendUint32(nums, uint32(n))
	}
	if len(nums) > 0 {
		put(scmRights, nums)
	}
	return out, ctrunc
}
