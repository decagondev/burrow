package netstack

// lease.go - keeping a DHCP lease once the stack runs (W23-S05): at each
// renewal time a REQUEST goes to the server from the leased address over the
// stack's own UDP (no broadcast needed then), and the answer's times set the
// next one. A server that does not answer is asked again later; the address
// is kept meanwhile (no rebinding or expiry). Never changes the address.
// Docs: docs/04-plan/waves/WAVE-23-proc-virtio.md.

import (
	"sync"
	"time"

	"gvisor.dev/gvisor/pkg/tcpip"
	"gvisor.dev/gvisor/pkg/tcpip/adapters/gonet"
	"gvisor.dev/gvisor/pkg/tcpip/network/ipv4"
)

const retryRenew = time.Minute

// leaseState is the lease the stack holds, for Show.
type leaseState struct {
	mu    sync.Mutex
	lease *Lease
	got   time.Time
}

func (l *leaseState) set(x Lease) {
	l.mu.Lock()
	l.lease, l.got = &x, time.Now()
	l.mu.Unlock()
}

func (l *leaseState) get() (Lease, time.Time, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.lease == nil {
		return Lease{}, time.Time{}, false
	}
	return *l.lease, l.got, true
}

// udpLink is DHCP over the stack's UDP, from the leased address to its server.
type udpLink struct{ c *gonet.UDPConn }

func (u udpLink) Send(msg []byte) error { _, err := u.c.Write(msg); return err }
func (u udpLink) Recv(deadline time.Time) ([]byte, error) {
	u.c.SetReadDeadline(deadline)
	b := make([]byte, 1500)
	n, err := u.c.Read(b)
	return b[:n], err
}

// KeepLease records l and renews it at its renewal times, forever; report
// hears each renewal's outcome.
func (s *Stack) KeepLease(l Lease, xid uint32, report func(Lease, error)) {
	s.lease.set(l)
	go func() {
		for {
			time.Sleep(max(l.Renew, retryRenew))
			xid++
			next, err := s.renew(l, xid)
			if report != nil {
				report(next, err)
			}
			if err == nil {
				l = next
				s.lease.set(l)
			}
		}
	}()
}

func (s *Stack) renew(l Lease, xid uint32) (Lease, error) {
	c, err := gonet.DialUDP(s.Stack, &tcpip.FullAddress{NIC: EthNIC, Addr: Addr(l.Addr.Addr()), Port: 68},
		&tcpip.FullAddress{NIC: EthNIC, Addr: Addr(l.Server), Port: 67}, ipv4.ProtocolNumber)
	if err != nil {
		return Lease{}, err
	}
	defer c.Close()
	return Renew(udpLink{c}, s.dev.MAC(), xid, l)
}
