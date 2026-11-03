package proc

// eventfd.go - eventfd2(2) (W12-S02): a 64-bit counter as a file. A read
// takes the counter (or 1 with EFD_SEMAPHORE), waiting while it is 0; a
// write adds, waiting while the sum would pass the maximum. Pollable, so
// epoll and O_NONBLOCK work on it (the Go runtime wakes its poller with one).
// Docs: docs/04-plan/waves/WAVE-12-ports.md.

import (
	"encoding/binary"
	"io/fs"
	"sync"
	"syscall"
	"time"
)

const (
	efdSemaphore = 1
	eventfdMax   = ^uint64(0) - 1
)

type eventfd struct {
	mu        sync.Mutex
	count     uint64
	semaphore bool
	changed   chan struct{}
}

func (e *eventfd) notifyLocked() { close(e.changed); e.changed = make(chan struct{}) }

func (e *eventfd) Read(b []byte) (int, error)  { return e.ReadCancel(b, nil) }
func (e *eventfd) Write(b []byte) (int, error) { return e.WriteCancel(b, nil) }

func (e *eventfd) ReadCancel(b []byte, cancel <-chan struct{}) (int, error) {
	if len(b) < 8 {
		return 0, syscall.EINVAL
	}
	for {
		e.mu.Lock()
		if e.count > 0 {
			v := e.count
			if e.semaphore {
				v = 1
			}
			e.count -= v
			e.notifyLocked()
			e.mu.Unlock()
			binary.LittleEndian.PutUint64(b, v)
			return 8, nil
		}
		ch := e.changed
		e.mu.Unlock()
		select {
		case <-ch:
		case <-cancel:
			return 0, syscall.EINTR
		}
	}
}

func (e *eventfd) WriteCancel(b []byte, cancel <-chan struct{}) (int, error) {
	if len(b) < 8 {
		return 0, syscall.EINVAL
	}
	v := binary.LittleEndian.Uint64(b)
	if v == ^uint64(0) {
		return 0, syscall.EINVAL
	}
	for {
		e.mu.Lock()
		if eventfdMax-e.count >= v {
			e.count += v
			e.notifyLocked()
			e.mu.Unlock()
			return 8, nil
		}
		ch := e.changed
		e.mu.Unlock()
		select {
		case <-ch:
		case <-cancel:
			return 0, syscall.EINTR
		}
	}
}

// Poll: readable with a count, writable while 1 more fits.
func (e *eventfd) Poll() (int16, <-chan struct{}) {
	e.mu.Lock()
	defer e.mu.Unlock()
	var rev int16
	if e.count > 0 {
		rev |= pollIn
	}
	if e.count < eventfdMax {
		rev |= pollOut
	}
	return rev, e.changed
}

func (e *eventfd) Close() error               { return nil }
func (e *eventfd) Stat() (fs.FileInfo, error) { return anonInfo("eventfd"), nil }

// anonInfo is the stat of a file with no path (eventfd, epoll).
type anonInfo string

func (a anonInfo) Name() string     { return "[" + string(a) + "]" }
func (anonInfo) Size() int64        { return 0 }
func (anonInfo) Mode() fs.FileMode  { return 0o600 }
func (anonInfo) ModTime() time.Time { return time.Time{} }
func (anonInfo) IsDir() bool        { return false }
func (anonInfo) Sys() any           { return nil }

// eventfd2(initval, flags): EFD_SEMAPHORE, EFD_NONBLOCK, EFD_CLOEXEC.
func sysEventfd2Fn(p *Process, a [6]uint64) (int64, bool) {
	flags := int(a[1])
	if flags&^(efdSemaphore|oNonblock|oCloexec) != 0 {
		return ok(-int64(syscall.EINVAL))
	}
	f := newFile(&eventfd{count: uint64(uint32(a[0])), semaphore: flags&efdSemaphore != 0, changed: make(chan struct{})}, nil)
	f.flags = oRDWR | flags&oNonblock
	n, err := p.fds.add(f, 0, flags&oCloexec != 0)
	if err != nil {
		f.unref()
		return ok(errno(err))
	}
	return ok(int64(n))
}

func init() {
	table[290] = sysEventfd2Fn
	table[284] = func(p *Process, a [6]uint64) (int64, bool) { return sysEventfd2Fn(p, [6]uint64{a[0]}) } // eventfd
}
