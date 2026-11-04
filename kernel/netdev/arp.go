package netdev

// ARP over Ethernet for IPv4 (RFC 826), just enough for a card's first
// check (W17-S01): ask who has an address and read the answer. The network
// stack (W17-S02) does its own ARP; this one runs before it exists and
// never caches anything.

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"net/netip"
	"time"
)

const (
	etherTypeARP = 0x0806
	ethHeader    = 14
	arpBody      = 28
	arpRequest   = 1
	arpReply     = 2
	hwEthernet   = 1
	protoIPv4    = 0x0800
	minFrame     = 60 // Ethernet's smallest frame without the CRC
)

// Broadcast is the all-ones Ethernet address.
var Broadcast = MAC{0xff, 0xff, 0xff, 0xff, 0xff, 0xff}

func (m MAC) String() string {
	return fmt.Sprintf("%02x:%02x:%02x:%02x:%02x:%02x", m[0], m[1], m[2], m[3], m[4], m[5])
}

// ARPRequest is a broadcast frame asking who has ip, from mac at src.
func ARPRequest(mac MAC, src, ip netip.Addr) []byte {
	f := make([]byte, minFrame) // padded to the minimum: the card does not pad
	copy(f[0:6], Broadcast[:])
	copy(f[6:12], mac[:])
	binary.BigEndian.PutUint16(f[12:], etherTypeARP)
	a := f[ethHeader:]
	binary.BigEndian.PutUint16(a[0:], hwEthernet)
	binary.BigEndian.PutUint16(a[2:], protoIPv4)
	a[4], a[5] = 6, 4
	binary.BigEndian.PutUint16(a[6:], arpRequest)
	copy(a[8:14], mac[:])
	s, t := src.As4(), ip.As4()
	copy(a[14:18], s[:])
	copy(a[24:28], t[:]) // the target's hardware address stays zero
	return f
}

// ARPReplyFrom reads frame as an ARP reply from ip and returns the address
// it gives; ok is false for any other frame.
func ARPReplyFrom(frame []byte, ip netip.Addr) (mac MAC, ok bool) {
	if len(frame) < ethHeader+arpBody || binary.BigEndian.Uint16(frame[12:]) != etherTypeARP {
		return MAC{}, false
	}
	a := frame[ethHeader:]
	t := ip.As4()
	if binary.BigEndian.Uint16(a[0:]) != hwEthernet || binary.BigEndian.Uint16(a[2:]) != protoIPv4 ||
		a[4] != 6 || a[5] != 4 || binary.BigEndian.Uint16(a[6:]) != arpReply || !bytes.Equal(a[14:18], t[:]) {
		return MAC{}, false
	}
	copy(mac[:], a[8:14])
	return mac, true
}

// Resolve asks d who has ip (from src), tries times times, waiting each wait
// for the answer, and restores no receiver: it is for use before anything
// else listens on d.
func Resolve(d Device, src, ip netip.Addr, tries int, wait time.Duration) (MAC, error) {
	got := make(chan MAC, 1)
	d.SetReceiver(func(frame []byte) {
		if mac, ok := ARPReplyFrom(frame, ip); ok {
			select {
			case got <- mac:
			default:
			}
		}
	})
	defer d.SetReceiver(nil)
	for i := 0; i < tries; i++ {
		if err := d.Send(ARPRequest(d.MAC(), src, ip)); err != nil && !errors.Is(err, ErrBusy) {
			return MAC{}, err
		}
		select {
		case mac := <-got:
			return mac, nil
		case <-time.After(wait):
		}
	}
	return MAC{}, fmt.Errorf("no answer from %v after %d tries", ip, tries)
}
