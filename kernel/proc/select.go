package proc

// select.go - select(2) and pselect6(2) (W14-S03): the fd_sets become
// pollfds and wait as poll does, so every Poller works with all three. The
// time left is not written back into the timeout (Linux does; musl and Qt
// do not read it), and pselect6's signal mask is ignored, as ppoll's is.
// Never holds a file's lock while blocked. Docs: POSIX_COVERAGE.md.

import (
	"encoding/binary"
	"syscall"
	"time"
)

const pollPri = 0x002

// What makes a descriptor ready in each set, as Linux's POLLIN_SET,
// POLLOUT_SET and POLLEX_SET (a hang-up also wakes a writer here, so a
// hang-up never ends a select with nothing set).
var setEvents = [3]int16{pollIn, pollOut, pollPri}
var setReady = [3]int16{pollIn | pollHup | pollErr, pollOut | pollErr | pollHup, pollPri}

func init() {
	ttable[23] = sysSelectFn
	ttable[270] = sysPselect6Fn
}

// select(nfds, readfds, writefds, exceptfds, struct timeval *timeout).
func sysSelectFn(t *Thread, a [6]uint64) (int64, bool) {
	timeout, e := t.p.timeArg(a[4], time.Microsecond)
	if e < 0 {
		return ok(e)
	}
	return ok(t.sel(a[0], [3]uint64{a[1], a[2], a[3]}, timeout))
}

// pselect6(nfds, readfds, writefds, exceptfds, struct timespec *, sigmask).
func sysPselect6Fn(t *Thread, a [6]uint64) (int64, bool) {
	timeout, e := t.p.timeArg(a[4], time.Nanosecond)
	if e < 0 {
		return ok(e)
	}
	return ok(t.sel(a[0], [3]uint64{a[1], a[2], a[3]}, timeout))
}

// timeArg reads {seconds, fraction in unit} at va; NULL means forever (-1).
func (p *Process) timeArg(va uint64, unit time.Duration) (time.Duration, int64) {
	if va == 0 {
		return -1, 0
	}
	var b [16]byte
	if err := p.mem.CopyIn(va, b[:]); err != nil {
		return 0, errno(err)
	}
	sec, frac := int64(binary.LittleEndian.Uint64(b[0:])), int64(binary.LittleEndian.Uint64(b[8:]))
	if sec < 0 || frac < 0 || frac >= int64(time.Second/unit) {
		return 0, -int64(syscall.EINVAL)
	}
	return time.Duration(sec)*time.Second + time.Duration(frac)*unit, 0
}

// sel waits on the descriptors below nfds named in the three sets (a set
// address may be 0), leaves only the ready ones set, and returns how many
// bits are set; -EBADF names a closed descriptor.
func (t *Thread) sel(nfds uint64, sets [3]uint64, timeout time.Duration) int64 {
	p := t.p
	n := int64(int32(nfds))
	if n < 0 {
		return -int64(syscall.EINVAL)
	}
	n = min(n, maxFDs)
	var bits [3][]byte
	for i, va := range sets {
		if va != 0 {
			bits[i] = make([]byte, (n+63)/64*8) // whole longs, as the kernel copies
			if err := p.mem.CopyIn(va, bits[i]); err != nil {
				return errno(err)
			}
		}
	}
	buf, fds := pollfdsOf(bits, int(n))
	if r := t.wait(buf, timeout); r < 0 {
		return r
	}
	count := int64(0)
	for i := range bits {
		clear(bits[i])
	}
	for k, fd := range fds {
		events := int16(binary.LittleEndian.Uint16(buf[k*pollfdSize+4:]))
		rev := int16(binary.LittleEndian.Uint16(buf[k*pollfdSize+6:]))
		if rev&pollNval != 0 {
			return -int64(syscall.EBADF)
		}
		for i := range bits {
			if events&setEvents[i] != 0 && rev&setReady[i] != 0 {
				bits[i][fd/8] |= 1 << (fd % 8)
				count++
			}
		}
	}
	for i, va := range sets {
		if va != 0 {
			if err := p.mem.CopyOut(va, bits[i]); err != nil {
				return errno(err)
			}
		}
	}
	return count
}

// pollfdsOf turns the sets into pollfds, one per descriptor named in any.
func pollfdsOf(bits [3][]byte, n int) ([]byte, []int) {
	var buf []byte
	var fds []int
	for fd := 0; fd < n; fd++ {
		var events int16
		for i, b := range bits {
			if b != nil && b[fd/8]>>(fd%8)&1 != 0 {
				events |= setEvents[i]
			}
		}
		if events == 0 {
			continue
		}
		var e [pollfdSize]byte
		binary.LittleEndian.PutUint32(e[0:], uint32(fd))
		binary.LittleEndian.PutUint16(e[4:], uint16(events))
		buf = append(buf, e[:]...)
		fds = append(fds, fd)
	}
	return buf, fds
}
