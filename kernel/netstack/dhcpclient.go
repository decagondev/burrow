package netstack

// dhcpclient.go - getting and keeping a DHCP lease (W23-S05): DISCOVER,
// OFFER, REQUEST, ACK over a Link, tried a few times and given up on after
// a deadline (a network without a server boots on); renewing is a REQUEST
// for the address held, sent to its server. FrameLink carries the first
// exchange as raw broadcast frames on the card, before the stack has an
// address. Never changes the stack itself: the caller applies the lease.
// Docs: docs/04-plan/waves/WAVE-23-proc-virtio.md.

import (
	"encoding/binary"
	"errors"
	"fmt"
	"net/netip"
	"time"

	"burrow/kernel/netdev"
)

// Link carries DHCP messages: Send one, Recv the next one for us by deadline.
type Link interface {
	Send(msg []byte) error
	Recv(deadline time.Time) ([]byte, error)
}

const (
	dhcpTries = 4
	dhcpWait  = 2 * time.Second // per try
)

// ErrNoLease is Acquire's answer when no server gave an address in time.
var ErrNoLease = errors.New("dhcp: no lease")

// Acquire gets a lease for the card mac: an OFFER, then the ACK for it.
func Acquire(link Link, mac netdev.MAC, xid uint32) (Lease, error) {
	for try := 0; try < dhcpTries; try++ {
		offer, err := exchange(link, mac, xid, dhcpMessage(dhcpDiscover, xid, mac, netip.Addr{}, netip.Addr{}, netip.Addr{}), dhcpOffer)
		if err != nil {
			continue
		}
		req := dhcpMessage(dhcpRequest, xid, mac, netip.Addr{}, offer.Addr.Addr(), offer.Server)
		ack, err := exchange(link, mac, xid, req, dhcpAck)
		if err == nil {
			return ack, nil
		}
		xid++ // a NAK or silence: start again
	}
	return Lease{}, ErrNoLease
}

// Renew asks the lease's server to extend it.
func Renew(link Link, mac netdev.MAC, xid uint32, l Lease) (Lease, error) {
	req := dhcpMessage(dhcpRequest, xid, mac, l.Addr.Addr(), netip.Addr{}, netip.Addr{})
	return exchange(link, mac, xid, req, dhcpAck)
}

// exchange sends msg and waits for a reply of type want to transaction xid;
// a NAK ends it at once.
func exchange(link Link, mac netdev.MAC, xid uint32, msg []byte, want byte) (Lease, error) {
	if err := link.Send(msg); err != nil {
		return Lease{}, err
	}
	deadline := time.Now().Add(dhcpWait)
	for {
		b, err := link.Recv(deadline)
		if err != nil {
			return Lease{}, err
		}
		r, err := parseReply(b, mac)
		switch {
		case err != nil || r.xid != xid:
			continue // someone else's, or noise
		case r.typ == dhcpNak:
			return Lease{}, errors.New("dhcp: the server said no")
		case r.typ == want:
			return r.lease, nil
		}
	}
}

// frameLink is DHCP on raw frames: broadcast from 0.0.0.0:68 to
// 255.255.255.255:67, and every UDP datagram to port 68 taken back.
type frameLink struct {
	dev netdev.Device
	in  chan []byte
}

// FrameLink takes the card's frames until Close (the stack takes them after).
func FrameLink(dev netdev.Device) *frameLink {
	l := &frameLink{dev: dev, in: make(chan []byte, 16)}
	dev.SetReceiver(func(f []byte) {
		if p, ok := udpTo68(f); ok {
			select {
			case l.in <- p:
			default:
			}
		}
	})
	return l
}

// Close gives the card's frames back.
func (l *frameLink) Close() { l.dev.SetReceiver(nil) }

func (l *frameLink) Send(msg []byte) error {
	mac := l.dev.MAC()
	return l.dev.Send(broadcastFrame(mac, msg))
}

func (l *frameLink) Recv(deadline time.Time) ([]byte, error) {
	select {
	case p := <-l.in:
		return p, nil
	case <-time.After(time.Until(deadline)):
		return nil, fmt.Errorf("dhcp: no answer: %w", ErrNoLease)
	}
}

// broadcastFrame is msg in UDP 68->67 in IPv4 0.0.0.0->255.255.255.255 in
// Ethernet to ff:ff:ff:ff:ff:ff (the UDP checksum is 0: none, as IPv4 allows).
func broadcastFrame(src netdev.MAC, msg []byte) []byte {
	f := make([]byte, 14+20+8+len(msg))
	copy(f[0:6], []byte{0xff, 0xff, 0xff, 0xff, 0xff, 0xff})
	copy(f[6:12], src[:])
	binary.BigEndian.PutUint16(f[12:], 0x0800)
	ip := f[14:34]
	ip[0], ip[8], ip[9] = 0x45, 64, 17 // IPv4, 20 bytes; TTL 64; UDP
	binary.BigEndian.PutUint16(ip[2:], uint16(20+8+len(msg)))
	copy(ip[16:20], []byte{255, 255, 255, 255})
	binary.BigEndian.PutUint16(ip[10:], ipChecksum(ip))
	udp := f[34:42]
	binary.BigEndian.PutUint16(udp[0:], 68)
	binary.BigEndian.PutUint16(udp[2:], 67)
	binary.BigEndian.PutUint16(udp[4:], uint16(8+len(msg)))
	copy(f[42:], msg)
	return f
}

func ipChecksum(h []byte) uint16 {
	var sum uint32
	for i := 0; i+1 < len(h); i += 2 {
		sum += uint32(binary.BigEndian.Uint16(h[i:]))
	}
	for sum>>16 != 0 {
		sum = sum&0xffff + sum>>16
	}
	return ^uint16(sum)
}

// udpTo68 is the payload of an IPv4 UDP frame to port 68.
func udpTo68(f []byte) ([]byte, bool) {
	if len(f) < 42 || binary.BigEndian.Uint16(f[12:]) != 0x0800 || f[14]>>4 != 4 || f[23] != 17 {
		return nil, false
	}
	ihl := int(f[14]&0xf) * 4
	if ihl < 20 || len(f) < 14+ihl+8 {
		return nil, false
	}
	udp := f[14+ihl:]
	n := int(binary.BigEndian.Uint16(udp[4:]))
	if binary.BigEndian.Uint16(udp[2:]) != 68 || n < 8 || n > len(udp) {
		return nil, false
	}
	return append([]byte(nil), udp[8:n]...), true
}
