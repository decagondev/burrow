package proc

// epoll.go - epoll_create1, epoll_ctl, epoll_wait, epoll_pwait (W12-S02):
// poll that remembers. An epoll file holds items (an fd, its open file and
// the events wanted) over files that implement Poller. Level-triggered
// items report while ready; an edge-triggered (EPOLLET) item reports when
// its file's change channel has closed since the last report, i.e. when
// something happened; EPOLLONESHOT disarms an item after one report. The
// Go runtime's network poller is the first user. A blocked wait also wakes
// on epoll_ctl ADD/MOD (the epoll's own change channel), so an item added by
// another thread is seen at once (BUG-0022). Docs: WAVE-12-ports.md.

import (
	"encoding/binary"
	"io/fs"
	"reflect"
	"sort"
	"sync"
	"syscall"
	"time"
)

const (
	epollIn      = 0x001
	epollOut     = 0x004
	epollErr     = 0x008
	epollHup     = 0x010
	epollRdhup   = 0x2000
	epollOneshot = 1 << 30
	epollET      = 1 << 31

	epollCtlAdd = 1
	epollCtlDel = 2
	epollCtlMod = 3

	epollEventSize = 12 // struct epoll_event { uint32 events; uint64 data; } packed on x86-64
	epollMaxEvents = 1024
)

type epollItem struct {
	file     *file
	events   uint32
	data     uint64
	lastSeen <-chan struct{} // edge-triggered: the change channel of the last report
	disarmed bool            // EPOLLONESHOT after a report
}

type epoll struct {
	mu      sync.Mutex
	items   map[int]*epollItem // by fd
	changed chan struct{}      // closed (and replaced) by ADD and MOD
}

func newEpoll() *epoll {
	return &epoll{items: map[int]*epollItem{}, changed: make(chan struct{})}
}

// touched wakes waits blocked on the old set of items; ep.mu is held.
func (ep *epoll) touched() {
	close(ep.changed)
	ep.changed = make(chan struct{})
}

func (*epoll) Read([]byte) (int, error)   { return 0, syscall.EINVAL }
func (*epoll) Close() error               { return nil }
func (*epoll) Stat() (fs.FileInfo, error) { return anonInfo("eventpoll"), nil }

func sysEpollCreate1Fn(p *Process, a [6]uint64) (int64, bool) {
	if a[0]&^oCloexec != 0 {
		return ok(-int64(syscall.EINVAL))
	}
	f := newFile(newEpoll(), nil)
	n, err := p.fds.add(f, 0, a[0]&oCloexec != 0)
	if err != nil {
		f.unref()
		return ok(errno(err))
	}
	return ok(int64(n))
}

func (p *Process) epollOf(fd uint64) (*epoll, int64) {
	f := p.fds.get(fd)
	if f == nil {
		return nil, -int64(syscall.EBADF)
	}
	ep, isEp := f.f.(*epoll)
	if !isEp {
		return nil, -int64(syscall.EINVAL)
	}
	return ep, 0
}

// epoll_ctl(epfd, op, fd, event)
func sysEpollCtlFn(p *Process, a [6]uint64) (int64, bool) {
	ep, e := p.epollOf(a[0])
	if e != 0 {
		return ok(e)
	}
	fd := int(int32(a[2]))
	f := p.fds.get(uint64(fd))
	if f == nil || fd < 0 {
		return ok(-int64(syscall.EBADF))
	}
	if _, pollable := device(f).(Poller); !pollable {
		return ok(-int64(syscall.EPERM)) // regular files and directories, as Linux
	}
	var ev [epollEventSize]byte
	if a[1] != epollCtlDel {
		if err := p.mem.CopyIn(a[3], ev[:]); err != nil {
			return ok(errno(err))
		}
	}
	events, data := binary.LittleEndian.Uint32(ev[0:]), binary.LittleEndian.Uint64(ev[4:])
	ep.mu.Lock()
	defer ep.mu.Unlock()
	it, exists := ep.items[fd]
	switch {
	case a[1] == epollCtlAdd && exists:
		return ok(-int64(syscall.EEXIST))
	case a[1] == epollCtlAdd:
		ep.items[fd] = &epollItem{file: f, events: events, data: data}
		ep.touched()
	case !exists && (a[1] == epollCtlMod || a[1] == epollCtlDel):
		return ok(-int64(syscall.ENOENT))
	case a[1] == epollCtlMod:
		*it = epollItem{file: f, events: events, data: data}
		ep.touched()
	case a[1] == epollCtlDel:
		delete(ep.items, fd)
	default:
		return ok(-int64(syscall.EINVAL))
	}
	return ok(0)
}

func sysEpollWaitFn(t *Thread, a [6]uint64) (int64, bool) {
	return ok(t.epollWait(a[0], a[1], int(int32(a[2])), int32(a[3])))
}

// epoll_pwait: as epoll_wait; the signal mask argument is ignored.
func sysEpollPwaitFn(t *Thread, a [6]uint64) (int64, bool) { return sysEpollWaitFn(t, a) }

// epollWait reports up to max ready items at va, waiting up to timeoutMs
// (negative: forever) for one; EINTR on a signal.
func (t *Thread) epollWait(epfd, va uint64, max int, timeoutMs int32) int64 {
	p := t.p
	ep, e := p.epollOf(epfd)
	if e != 0 {
		return e
	}
	if max <= 0 || max > epollMaxEvents {
		return -int64(syscall.EINVAL)
	}
	var deadline <-chan time.Time
	if timeoutMs > 0 {
		tm := time.NewTimer(time.Duration(timeoutMs) * time.Millisecond)
		defer tm.Stop()
		deadline = tm.C
	}
	for {
		out, waits := ep.scan(p, max)
		if len(out) > 0 || timeoutMs == 0 {
			if err := p.mem.CopyOut(va, out); err != nil {
				return errno(err)
			}
			return int64(len(out) / epollEventSize)
		}
		cases := []reflect.SelectCase{
			{Dir: reflect.SelectRecv, Chan: reflect.ValueOf(t.cancel())},
			{Dir: reflect.SelectRecv, Chan: reflect.ValueOf(deadline)},
		}
		for _, ch := range waits {
			cases = append(cases, reflect.SelectCase{Dir: reflect.SelectRecv, Chan: reflect.ValueOf(ch)})
		}
		switch chosen, _, _ := reflect.Select(cases); chosen {
		case 0:
			return -int64(syscall.EINTR)
		case 1:
			return 0
		}
	}
}

// scan collects up to max ready items as epoll_events and the channels to
// wait on otherwise (the epoll's own first). Items whose fd was closed or
// reused are dropped.
func (ep *epoll) scan(p *Process, max int) ([]byte, []<-chan struct{}) {
	ep.mu.Lock()
	defer ep.mu.Unlock()
	fds := make([]int, 0, len(ep.items))
	for fd := range ep.items {
		fds = append(fds, fd)
	}
	sort.Ints(fds)
	var out []byte
	waits := []<-chan struct{}{ep.changed}
	for _, fd := range fds {
		it := ep.items[fd]
		if p.fds.get(uint64(fd)) != it.file {
			delete(ep.items, fd) // closed: Linux drops it with the last reference
			continue
		}
		if it.disarmed {
			continue
		}
		rev, ch := device(it.file).(Poller).Poll()
		waits = append(waits, ch)
		ready := uint32(uint16(rev)) & (it.events | epollErr | epollHup)
		if rev&pollHup != 0 && it.events&epollRdhup != 0 {
			ready |= epollRdhup
		}
		if ready == 0 || len(out)/epollEventSize >= max {
			continue
		}
		if it.events&epollET != 0 {
			if it.lastSeen != nil && !closed(it.lastSeen) {
				continue // nothing happened since the last report
			}
			it.lastSeen = ch
		}
		if it.events&epollOneshot != 0 {
			it.disarmed = true
		}
		var ev [epollEventSize]byte
		binary.LittleEndian.PutUint32(ev[0:], ready)
		binary.LittleEndian.PutUint64(ev[4:], it.data)
		out = append(out, ev[:]...)
	}
	return out, waits
}

func closed(ch <-chan struct{}) bool {
	select {
	case <-ch:
		return true
	default:
		return false
	}
}

func init() {
	table[291] = sysEpollCreate1Fn
	table[213] = func(p *Process, a [6]uint64) (int64, bool) { // epoll_create(size)
		if int32(a[0]) <= 0 {
			return ok(-int64(syscall.EINVAL))
		}
		return sysEpollCreate1Fn(p, [6]uint64{})
	}
	table[233] = sysEpollCtlFn
	ttable[232] = sysEpollWaitFn
	ttable[281] = sysEpollPwaitFn
}
