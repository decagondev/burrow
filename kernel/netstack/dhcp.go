package netstack

// dhcp.go - DHCPv4 messages (W23-S05, RFC 2131 and 2132): a DISCOVER and a
// REQUEST built for a card's address, and a server's OFFER, ACK or NAK
// parsed into a Lease. Pure: no network here (dhcpclient.go sends them).
// Never trusts a reply's lengths: a malformed message is an error.
// Docs: docs/04-plan/waves/WAVE-23-proc-virtio.md.

import (
	"encoding/binary"
	"errors"
	"net/netip"
	"time"

	"burrow/kernel/netdev"
)

// DHCP message types (option 53).
const (
	dhcpDiscover = 1
	dhcpOffer    = 2
	dhcpRequest  = 3
	dhcpAck      = 5
	dhcpNak      = 6
)

const (
	bootRequest, bootReply = 1, 2
	bootpBytes             = 236 // the fixed part, before the magic cookie
	optPad, optEnd         = 0, 255
	optMask, optRouter     = 1, 3
	optDNS, optRequested   = 6, 50
	optLease, optType      = 51, 53
	optServer, optParams   = 54, 55
	optRenewal             = 58
	flagBroadcast          = 0x8000
)

var magicCookie = [4]byte{99, 130, 83, 99}

// Lease is what a server gave: the address with its subnet, the gateway,
// DNS servers, who gave it and for how long (Renew: when to ask again).
type Lease struct {
	Addr    netip.Prefix
	Gateway netip.Addr
	DNS     []netip.Addr
	Server  netip.Addr
	Time    time.Duration
	Renew   time.Duration
}

// dhcpMessage builds a client message of type typ: ciaddr is our address
// when renewing; requested and server go into options 50 and 54 when valid.
func dhcpMessage(typ byte, xid uint32, mac netdev.MAC, ciaddr, requested, server netip.Addr) []byte {
	b := make([]byte, bootpBytes, bootpBytes+64)
	b[0], b[1], b[2] = bootRequest, 1, 6 // Ethernet, 6-byte addresses
	binary.BigEndian.PutUint32(b[4:], xid)
	if !ciaddr.IsValid() {
		binary.BigEndian.PutUint16(b[10:], flagBroadcast) // no address yet: answer by broadcast
	} else {
		a := ciaddr.As4()
		copy(b[12:], a[:])
	}
	copy(b[28:], mac[:])
	b = append(b, magicCookie[:]...)
	b = append(b, optType, 1, typ)
	for _, o := range []struct {
		code byte
		addr netip.Addr
	}{{optRequested, requested}, {optServer, server}} {
		if o.addr.IsValid() {
			a := o.addr.As4()
			b = append(b, o.code, 4, a[0], a[1], a[2], a[3])
		}
	}
	b = append(b, optParams, 4, optMask, optRouter, optDNS, optLease)
	return append(b, optEnd)
}

// reply is a server's message: its type, the transaction, and the lease.
type reply struct {
	typ   byte
	xid   uint32
	lease Lease
}

// parseReply reads a server's message for the client with mac.
func parseReply(b []byte, mac netdev.MAC) (reply, error) {
	if len(b) < bootpBytes+4 || b[0] != bootReply || [4]byte(b[bootpBytes:bootpBytes+4]) != magicCookie {
		return reply{}, errors.New("dhcp: not a BOOTP reply")
	}
	if [6]byte(b[28:34]) != mac {
		return reply{}, errors.New("dhcp: a reply for another card")
	}
	r := reply{xid: binary.BigEndian.Uint32(b[4:])}
	yiaddr := netip.AddrFrom4([4]byte(b[16:20]))
	mask := 24
	opts := b[bootpBytes+4:]
	for len(opts) > 0 && opts[0] != optEnd {
		if opts[0] == optPad {
			opts = opts[1:]
			continue
		}
		if len(opts) < 2 || len(opts) < 2+int(opts[1]) {
			return reply{}, errors.New("dhcp: an option runs past the end")
		}
		code, val := opts[0], opts[2:2+int(opts[1])]
		opts = opts[2+len(val):]
		r.option(code, val, &mask)
	}
	if r.typ == 0 {
		return reply{}, errors.New("dhcp: no message type")
	}
	r.lease.Addr = netip.PrefixFrom(yiaddr, mask)
	if r.lease.Renew == 0 {
		r.lease.Renew = r.lease.Time / 2 // T1's default
	}
	return r, nil
}

// option records one option of a reply.
func (r *reply) option(code byte, val []byte, mask *int) {
	addr := func(i int) netip.Addr { return netip.AddrFrom4([4]byte(val[4*i : 4*i+4])) }
	secs := func() time.Duration { return time.Duration(binary.BigEndian.Uint32(val)) * time.Second }
	switch {
	case code == optType && len(val) == 1:
		r.typ = val[0]
	case code == optMask && len(val) == 4:
		*mask, _ = netMaskBits(val)
	case code == optRouter && len(val) >= 4:
		r.lease.Gateway = addr(0)
	case code == optDNS && len(val)%4 == 0:
		for i := 0; i < len(val)/4; i++ {
			r.lease.DNS = append(r.lease.DNS, addr(i))
		}
	case code == optServer && len(val) == 4:
		r.lease.Server = addr(0)
	case code == optLease && len(val) == 4:
		r.lease.Time = secs()
	case code == optRenewal && len(val) == 4:
		r.lease.Renew = secs()
	}
}

// netMaskBits is a mask's prefix length (24 for 255.255.255.0).
func netMaskBits(m []byte) (int, bool) {
	v := binary.BigEndian.Uint32(m)
	n := 0
	for v&0x80000000 != 0 {
		n++
		v <<= 1
	}
	return n, v == 0
}
