// Package netstack is Burrow's TCP/IP (W17-S02): gVisor's stack
// (gvisor.dev/gvisor/pkg/tcpip, vendored at a pinned version: IPv4, IPv6,
// ARP, ICMP, TCP, UDP) with a loopback interface and eth0, a netdev card
// joined to it through gVisor's channel and ethernet link endpoints. Frames
// cross between them on two pumps: the card's receiver injects every frame,
// and a goroutine hands every outgoing one to the card. Never touches a
// card's registers; the driver does (kernel/drivers/e1000). Docs:
// docs/04-plan/waves/WAVE-17-networking.md, ADR-0009.
package netstack

import (
	"context"
	"fmt"
	"net/netip"
	"time"

	"gvisor.dev/gvisor/pkg/buffer"
	"gvisor.dev/gvisor/pkg/tcpip"
	"gvisor.dev/gvisor/pkg/tcpip/header"
	"gvisor.dev/gvisor/pkg/tcpip/link/channel"
	"gvisor.dev/gvisor/pkg/tcpip/link/ethernet"
	"gvisor.dev/gvisor/pkg/tcpip/link/loopback"
	"gvisor.dev/gvisor/pkg/tcpip/network/arp"
	"gvisor.dev/gvisor/pkg/tcpip/network/ipv4"
	"gvisor.dev/gvisor/pkg/tcpip/network/ipv6"
	"gvisor.dev/gvisor/pkg/tcpip/stack"
	"gvisor.dev/gvisor/pkg/tcpip/transport/icmp"
	"gvisor.dev/gvisor/pkg/tcpip/transport/raw"
	"gvisor.dev/gvisor/pkg/tcpip/transport/tcp"
	"gvisor.dev/gvisor/pkg/tcpip/transport/udp"

	"burrow/kernel/netdev"
)

const (
	LoopbackNIC tcpip.NICID = 1
	EthNIC      tcpip.NICID = 2

	ethHeader  = 14
	queueDepth = 256 // outgoing packets waiting for the card
	busyWait   = 200 * time.Microsecond
)

// Stack is the kernel's network.
type Stack struct {
	*stack.Stack
	Config Config
	dev    netdev.Device
	link   *channel.Endpoint
	stop   context.CancelFunc
	lease  leaseState // eth0's DHCP lease, if it has one (lease.go, W23-S05)
}

// New makes a stack with loopback (127.0.0.1/8, ::1) and, if dev is not
// nil, eth0 on dev with cfg's address and routes.
func New(dev netdev.Device, cfg Config) (*Stack, error) {
	s := &Stack{Config: cfg, dev: dev}
	s.Stack = stack.New(stack.Options{
		NetworkProtocols:   []stack.NetworkProtocolFactory{ipv4.NewProtocol, ipv6.NewProtocol, arp.NewProtocol},
		TransportProtocols: []stack.TransportProtocolFactory{tcp.NewProtocol, udp.NewProtocol, icmp.NewProtocol4, icmp.NewProtocol6},
		HandleLocal:        true,
		RawFactory:         raw.EndpointFactory{}, // raw ICMP sockets: busybox ping (W17-S04)
	})
	if err := s.loopback(); err != nil {
		return nil, err
	}
	if dev != nil {
		if err := s.ethernet(); err != nil {
			return nil, err
		}
	}
	s.routes()
	return s, nil
}

func (s *Stack) loopback() error {
	if err := s.CreateNICWithOptions(LoopbackNIC, loopback.New(), stack.NICOptions{Name: "lo"}); err != nil {
		return fmt.Errorf("lo: %v", err)
	}
	for _, p := range []netip.Prefix{netip.MustParsePrefix("127.0.0.1/8"), netip.MustParsePrefix("::1/128")} {
		if err := s.addAddress(LoopbackNIC, p); err != nil {
			return err
		}
	}
	return nil
}

func (s *Stack) ethernet() error {
	mac := s.dev.MAC()
	s.link = channel.New(queueDepth, uint32(s.dev.MTU()+ethHeader), tcpip.LinkAddress(mac[:]))
	if err := s.CreateNICWithOptions(EthNIC, ethernet.New(s.link), stack.NICOptions{Name: "eth0"}); err != nil {
		return fmt.Errorf("eth0: %v", err)
	}
	if err := s.addAddress(EthNIC, s.Config.Addr); err != nil {
		return err
	}
	ctx, stop := context.WithCancel(context.Background())
	s.stop = stop
	s.dev.SetReceiver(s.inbound)
	go s.outbound(ctx)
	return nil
}

func (s *Stack) addAddress(nic tcpip.NICID, p netip.Prefix) error {
	proto := tcpip.NetworkProtocolNumber(ipv4.ProtocolNumber)
	if p.Addr().Is6() {
		proto = ipv6.ProtocolNumber
	}
	pa := tcpip.ProtocolAddress{
		Protocol:          proto,
		AddressWithPrefix: tcpip.AddressWithPrefix{Address: Addr(p.Addr()), PrefixLen: p.Bits()},
	}
	if err := s.AddProtocolAddress(nic, pa, stack.AddressProperties{}); err != nil {
		return fmt.Errorf("address %v: %v", p, err)
	}
	return nil
}

// routes: loopback first, then eth0's subnet, then everything else to the
// gateway (gVisor takes the first route that matches).
func (s *Stack) routes() {
	sub := func(p string) tcpip.Subnet {
		pp := netip.MustParsePrefix(p)
		return tcpip.AddressWithPrefix{Address: Addr(pp.Addr()), PrefixLen: pp.Bits()}.Subnet()
	}
	table := []tcpip.Route{
		{Destination: sub("127.0.0.0/8"), NIC: LoopbackNIC},
		{Destination: sub("::1/128"), NIC: LoopbackNIC},
	}
	if s.dev != nil {
		p := s.Config.Addr.Masked()
		table = append(table, tcpip.Route{Destination: sub(p.String()), NIC: EthNIC})
		if s.Config.Gateway.IsValid() {
			table = append(table, tcpip.Route{Destination: header.IPv4EmptySubnet, Gateway: Addr(s.Config.Gateway), NIC: EthNIC})
		}
	}
	s.SetRouteTable(table)
}

// inbound runs on the driver's interrupt goroutine: the frame becomes a
// packet for the ethernet endpoint, which reads its header.
func (s *Stack) inbound(frame []byte) {
	pkt := stack.NewPacketBuffer(stack.PacketBufferOptions{Payload: buffer.MakeWithData(frame)})
	s.link.InjectInbound(0, pkt)
	pkt.DecRef()
}

// outbound hands every packet the stack sends to the card, waiting while
// the card's ring is full.
func (s *Stack) outbound(ctx context.Context) {
	for {
		pkt := s.link.ReadContext(ctx)
		if pkt == nil {
			return
		}
		view := pkt.ToView()
		frame := view.AsSlice()
		for s.dev.Send(frame) == netdev.ErrBusy {
			time.Sleep(busyWait)
		}
		view.Release()
		pkt.DecRef()
	}
}

// Close stops the pumps (host tests).
func (s *Stack) Close() {
	if s.stop != nil {
		s.dev.SetReceiver(nil)
		s.stop()
	}
	s.Stack.Close()
}

// Addr converts an address for gVisor.
func Addr(a netip.Addr) tcpip.Address { return tcpip.AddrFromSlice(a.AsSlice()) }
