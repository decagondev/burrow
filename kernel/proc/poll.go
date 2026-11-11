package proc

// poll.go - poll(2) and ppoll(2) (W10-S08: busybox's line editor waits in
// poll). A file that can change readiness implements Poller; any other file
// (regular files, the console) is always ready, as on Linux. Never holds a
// file's lock while blocked. Docs: docs/02-architecture/POSIX_COVERAGE.md.

import (
	"encoding/binary"
	"syscall"
	"time"
)

// poll(2) event bits (Linux).
const (
	pollIn   = 0x001
	pollOut  = 0x004
	pollErr  = 0x008
	pollHup  = 0x010
	pollNval = 0x020

	pollfdSize = 8 // struct pollfd { int fd; short events, revents; }
)

// Poller is a file whose readiness changes: Poll reports the bits that
// hold now and a channel closed at the next change.
type Poller interface {
	Poll() (revents int16, changed <-chan struct{})
}

// inputWaiter is a file that must know someone polls it for input (the
// terminal: the console only forwards keys while a program waits).
type inputWaiter interface{ WaitInput(delta int) }

func init() {
	ttable[7] = sysPollFn
	ttable[271] = sysPpollFn
}

func sysPollFn(t *Thread, a [6]uint64) (int64, bool) {
	timeout := time.Duration(-1) // negative ms: wait forever
	if ms := int32(a[2]); ms >= 0 {
		timeout = time.Duration(ms) * time.Millisecond
	}
	return ok(t.poll(a[0], a[1], timeout))
}

// ppoll: like poll with a timespec; the signal mask argument is ignored.
func sysPpollFn(t *Thread, a [6]uint64) (int64, bool) {
	timeout, e := t.p.timeArg(a[2], time.Nanosecond)
	if e < 0 {
		return ok(e)
	}
	return ok(t.poll(a[0], a[1], timeout))
}

// poll waits until one of the n pollfds at va is ready, the timeout passes
// (negative: never) or a signal arrives (EINTR); it returns how many are ready.
func (t *Thread) poll(va, n uint64, timeout time.Duration) int64 {
	p := t.p
	if n > maxFDs {
		return -int64(syscall.EINVAL)
	}
	buf := make([]byte, n*pollfdSize)
	if err := p.mem.CopyIn(va, buf); err != nil {
		return errno(err)
	}
	ready := t.wait(buf, timeout)
	if ready < 0 {
		return ready
	}
	if err := p.mem.CopyOut(va, buf); err != nil {
		return errno(err)
	}
	return ready
}

// wait is poll on pollfds in Go memory (select builds them too): it fills
// in every revents (all zero after a timeout) and returns how many are
// ready, or -EINTR.
func (t *Thread) wait(buf []byte, timeout time.Duration) int64 {
	p := t.p
	var deadline <-chan time.Time
	if timeout > 0 {
		t := time.NewTimer(timeout)
		defer t.Stop()
		deadline = t.C
	}
	for {
		ready, waits, waiters := p.scan(buf)
		if ready > 0 || timeout == 0 {
			return int64(ready)
		}
		for _, w := range waiters {
			w.WaitInput(1)
		}
		chosen := waitAny(t.cancel(), deadline, waits) // W25-S04: a plain select for a few files
		for _, w := range waiters {
			w.WaitInput(-1)
		}
		switch chosen {
		case 0:
			return -int64(syscall.EINTR)
		case 1:
			for off := 0; off < len(buf); off += pollfdSize {
				binary.LittleEndian.PutUint16(buf[off+6:], 0)
			}
			return 0
		}
	}
}

// scan fills in every revents; it returns how many are non-zero, the
// channels to wait on, and the files that want to know about a waiter.
func (p *Process) scan(buf []byte) (int, []<-chan struct{}, []inputWaiter) {
	ready := 0
	var waits []<-chan struct{}
	var waiters []inputWaiter
	for off := 0; off < len(buf); off += pollfdSize {
		e := buf[off : off+pollfdSize]
		fdn := int32(binary.LittleEndian.Uint32(e))
		events := int16(binary.LittleEndian.Uint16(e[4:]))
		var rev int16
		if fdn >= 0 {
			rev = pollNval
			if f := p.fds.get(uint64(fdn)); f != nil {
				rev = pollIn | pollOut // files and the console never block
				if pl, isP := device(f).(Poller); isP {
					var ch <-chan struct{}
					rev, ch = pl.Poll()
					waits = append(waits, ch)
					if w, isW := device(f).(inputWaiter); isW && events&pollIn != 0 {
						waiters = append(waiters, w)
					}
				}
				rev &= events | pollErr | pollHup
			}
		}
		binary.LittleEndian.PutUint16(e[6:], uint16(rev))
		if rev != 0 {
			ready++
		}
	}
	return ready, waits, waiters
}
