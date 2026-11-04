package proc

// socknames.go - named Unix sockets (W14-S07): socket, bind, listen,
// connect, accept/accept4, getsockname and getpeername. A path name is a
// socket node in a writable mount (bind fails with EADDRINUSE while
// anything is at the path, so servers unlink first, as on Linux); an
// abstract name (a leading NUL) lives only in the system's table. The
// table maps names to bound sockets; a node whose socket is gone refuses
// connections. A Unix connect completes or fails at once: the server end
// waits in the listener's queue until accept takes it. Only stream and
// seqpacket sockets take names; named datagram sockets are not supported
// (EOPNOTSUPP). Docs: docs/02-architecture/POSIX_COVERAGE.md.

import (
	"encoding/binary"
	"fmt"
	"io/fs"
	"sync"
	"syscall"
)

const (
	sockaddrMax = 110 // sizeof(struct sockaddr_un)
	somaxconn   = 4096
)

// listenQ is a listener's queue of connections nobody accepted yet.
type listenQ struct {
	backlog int
	pid     int32 // who listened: SO_PEERCRED for the clients
	pending []*sockEnd
	changed chan struct{}
}

func (q *listenQ) notify() {
	close(q.changed)
	q.changed = make(chan struct{})
}

// sockNames is the system's table of bound names.
type sockNames struct {
	mu    sync.Mutex
	bound map[string]*sockEnd // resolved path or abstract name -> socket
	auto  int                 // the next autobind name
}

func (n *sockNames) lookup(key string) *sockEnd {
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.bound[key]
}

func (n *sockNames) set(key string, e *sockEnd) {
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.bound == nil {
		n.bound = map[string]*sockEnd{}
	}
	n.bound[key] = e
}

// drop forgets every name e holds (its close).
func (n *sockNames) drop(e *sockEnd) {
	n.mu.Lock()
	defer n.mu.Unlock()
	for k, v := range n.bound {
		if v == e {
			delete(n.bound, k)
		}
	}
}

func (n *sockNames) autoName() string {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.auto++
	return fmt.Sprintf("\x00%05x", n.auto)
}

// socketMaker is a file system that can hold socket nodes (memfs).
type socketMaker interface{ MakeSocket(name string) error }

// sockaddr reads a struct sockaddr_un of n bytes at va: the name as given
// (path, "\x00..." or "" for a bare family) and its table key (the path
// resolved to an io/fs path; an abstract name is its own key).
func (p *Process) sockaddr(va, n uint64) (name, key string, e int64) {
	if n < 2 || n > sockaddrMax {
		return "", "", -int64(syscall.EINVAL)
	}
	b := make([]byte, n)
	if err := p.mem.CopyIn(va, b); err != nil {
		return "", "", errno(err)
	}
	if binary.LittleEndian.Uint16(b) != afUnix {
		return "", "", -int64(syscall.EINVAL)
	}
	raw := b[2:]
	switch {
	case len(raw) == 0:
		return "", "", 0
	case raw[0] == 0:
		return string(raw), string(raw), 0
	}
	for i, c := range raw {
		if c == 0 {
			raw = raw[:i]
			break
		}
	}
	return string(raw), p.resolve(string(raw)), 0
}

// sockaddrOf is the struct sockaddr_un for a name, as long as Linux makes it.
func sockaddrOf(name string) []byte {
	b := append([]byte{afUnix, 0}, name...)
	if name != "" && name[0] != 0 {
		b = append(b, 0)
	}
	return b
}

// putSockaddr writes name's address at va (cut to the room *lenVA says)
// and its full length at lenVA, as accept and getsockname do.
func (p *Process) putSockaddr(name string, va, lenVA uint64) int64 {
	return p.putAddr(sockaddrOf(name), va, lenVA)
}

// socket(domain, type, protocol); AF_INET and AF_INET6 are inetcalls.go's.
func sysSocketFn(p *Process, a [6]uint64) (int64, bool) {
	if a[0] == afInet || a[0] == afInet6 {
		return ok(p.inetSocket(a))
	}
	typ, e := sockType(a[0], a[1], a[2])
	if e < 0 {
		return ok(e)
	}
	f := newFile(newSockEnd(typ, p.sys), nil)
	f.flags = oRDWR
	if a[1]&sockNonblock != 0 {
		f.flags |= oNonblock
	}
	n, err := p.fds.add(f, 0, a[1]&sockCloexec != 0)
	if err != nil {
		f.unref()
		return ok(errno(err))
	}
	return ok(int64(n))
}

// bind(fd, addr, len): a bare family autobinds an abstract name.
func sysBindFn(p *Process, a [6]uint64) (int64, bool) {
	if is, _ := p.inetFD(a[0]); is != nil {
		return ok(p.inetBind(is, a[1], a[2]))
	}
	s, _, e := p.socket(a[0])
	if e < 0 {
		return ok(e)
	}
	name, key, e := p.sockaddr(a[1], a[2])
	switch {
	case e < 0:
		return ok(e)
	case s.typ == sockDgram:
		return ok(-int64(syscall.EOPNOTSUPP))
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.name != "" || s.pair() != nil {
		return ok(-int64(syscall.EINVAL))
	}
	if name == "" {
		name = p.sys.names.autoName()
		key = name
	}
	if name[0] == 0 {
		if p.sys.names.lookup(key) != nil {
			return ok(-int64(syscall.EADDRINUSE))
		}
	} else if e := p.makeSocketNode(key); e < 0 {
		return ok(e)
	}
	s.name = name
	p.sys.names.set(key, s)
	return ok(0)
}

// makeSocketNode creates the node for a path name (an io/fs path).
func (p *Process) makeSocketNode(rel string) int64 {
	if _, err := fs.Stat(p.sys.files, rel); err == nil {
		return -int64(syscall.EADDRINUSE)
	}
	m, ok := p.sys.files.(socketMaker)
	if !ok {
		return -int64(syscall.EROFS)
	}
	return errno(m.MakeSocket(rel))
}

// listen(fd, backlog).
func sysListenFn(p *Process, a [6]uint64) (int64, bool) {
	if is, _ := p.inetFD(a[0]); is != nil {
		return ok(is.listen(a[1]))
	}
	s, _, e := p.socket(a[0])
	switch {
	case e < 0:
		return ok(e)
	case s.typ == sockDgram:
		return ok(-int64(syscall.EOPNOTSUPP))
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.name == "" || s.pair() != nil {
		return ok(-int64(syscall.EINVAL))
	}
	backlog := min(max(int(int32(a[1])), 0), somaxconn)
	if s.lis == nil {
		s.lis = &listenQ{pid: int32(p.PID), changed: make(chan struct{})}
	}
	s.lis.backlog = backlog
	s.lis.notify()
	return ok(0)
}

// unlisten closes the connections nobody accepted (a listener's close).
func (e *sockEnd) unlisten() {
	e.mu.Lock()
	q := e.lis
	e.lis = nil
	var pending []*sockEnd
	if q != nil {
		pending = q.pending
		q.pending = nil
		q.notify()
	}
	e.mu.Unlock()
	for _, s := range pending {
		s.Close()
	}
}

func init() {
	for nr, h := range map[uint64]handler{
		41: sysSocketFn, 49: sysBindFn, 50: sysListenFn, 51: sysGetsocknameFn, 52: sysGetpeernameFn,
	} {
		table[nr] = h
	}
	for nr, h := range map[uint64]thandler{42: sysConnectFn, 43: sysAcceptFn, 288: sysAccept4Fn} {
		ttable[nr] = h
	}
}
