package proc

// hostname.go - the node and domain names uname reports (W17-S04), set by
// sethostname and setdomainname (everyone is root), "burrow" and "(none)"
// until then, as /etc/hostname says. One per system: there are no UTS
// namespaces. Docs: POSIX_COVERAGE.md.

import "syscall"

const (
	defaultHost   = "burrow"
	defaultDomain = "(none)"
	hostMax       = 64 // __NEW_UTS_LEN
)

// host is the node and domain names.
func (s *System) host() (node, domain string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	node, domain = s.hostname, s.domainname
	if node == "" {
		node = defaultHost
	}
	if domain == "" {
		domain = defaultDomain
	}
	return node, domain
}

// setName reads n bytes at va as a new name for *dst.
func (p *Process) setName(dst *string, va, n uint64) int64 {
	if n > hostMax {
		return -int64(syscall.EINVAL)
	}
	b := make([]byte, n)
	if err := p.mem.CopyIn(va, b); err != nil {
		return errno(err)
	}
	p.sys.mu.Lock()
	*dst = string(b)
	p.sys.mu.Unlock()
	return 0
}

// sethostname(name, len) and setdomainname(name, len).
func sysSethostnameFn(p *Process, a [6]uint64) (int64, bool) {
	return ok(p.setName(&p.sys.hostname, a[0], a[1]))
}

func sysSetdomainnameFn(p *Process, a [6]uint64) (int64, bool) {
	return ok(p.setName(&p.sys.domainname, a[0], a[1]))
}

func init() {
	table[170] = sysSethostnameFn
	table[171] = sysSetdomainnameFn
}
