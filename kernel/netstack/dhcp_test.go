package netstack

import (
	"encoding/binary"
	"errors"
	"net/netip"
	"strings"
	"testing"
	"time"

	"burrow/kernel/netdev"
)

var cardMAC = netdev.MAC{0x52, 0x54, 0, 0x12, 0x34, 0x56}

// server is a scripted DHCP server: each client message gets the replies
// answer gives (none: the client times out at once).
type server struct {
	sent   [][]byte
	queue  [][]byte
	answer func(typ byte, msg []byte) [][]byte
}

func (s *server) Send(msg []byte) error {
	s.sent = append(s.sent, msg)
	s.queue = append(s.queue, s.answer(msgType(msg), msg)...)
	return nil
}

func (s *server) Recv(time.Time) ([]byte, error) {
	if len(s.queue) == 0 {
		return nil, ErrNoLease
	}
	b := s.queue[0]
	s.queue = s.queue[1:]
	return b, nil
}

// msgType is option 53 of a client message.
func msgType(b []byte) byte { return b[bootpBytes+4+2] }

// option is the value of code in a client message, or nil.
func option(b []byte, code byte) []byte {
	o := b[bootpBytes+4:]
	for len(o) > 1 && o[0] != optEnd {
		if o[0] == code {
			return o[2 : 2+o[1]]
		}
		o = o[2+o[1]:]
	}
	return nil
}

// serverReply is a server's message of type typ answering msg.
func serverReply(typ byte, msg []byte, mac netdev.MAC) []byte {
	b := make([]byte, bootpBytes)
	b[0] = bootReply
	copy(b[4:8], msg[4:8]) // the client's xid
	copy(b[16:20], []byte{10, 0, 2, 15})
	copy(b[28:], mac[:])
	b = append(b, magicCookie[:]...)
	b = append(b, optPad, optType, 1, typ, optMask, 4, 255, 255, 255, 0, optRouter, 4, 10, 0, 2, 2,
		optDNS, 8, 10, 0, 2, 3, 1, 1, 1, 1, optServer, 4, 10, 0, 2, 2, optLease, 4, 0, 1, 0x51, 0x80, optEnd)
	return b
}

func slirp(typ byte, msg []byte) [][]byte {
	switch typ {
	case dhcpDiscover:
		return [][]byte{serverReply(dhcpOffer, msg, cardMAC)}
	case dhcpRequest:
		return [][]byte{serverReply(dhcpAck, msg, cardMAC)}
	}
	return nil
}

func TestAcquire(t *testing.T) {
	s := &server{answer: slirp}
	l, err := Acquire(s, cardMAC, 0x1234)
	if err != nil {
		t.Fatal(err)
	}
	if l.Addr != netip.MustParsePrefix("10.0.2.15/24") || l.Gateway != netip.MustParseAddr("10.0.2.2") ||
		len(l.DNS) != 2 || l.DNS[0] != netip.MustParseAddr("10.0.2.3") || l.Server != netip.MustParseAddr("10.0.2.2") ||
		l.Time != 86400*time.Second || l.Renew != 43200*time.Second {
		t.Fatalf("lease %+v", l)
	}
	disc, req := s.sent[0], s.sent[1]
	if msgType(disc) != dhcpDiscover || binary.BigEndian.Uint16(disc[10:]) != flagBroadcast || [6]byte(disc[28:34]) != cardMAC {
		t.Errorf("discover % x", disc[:40])
	}
	if msgType(req) != dhcpRequest || string(option(req, optRequested)) != string([]byte{10, 0, 2, 15}) ||
		string(option(req, optServer)) != string([]byte{10, 0, 2, 2}) {
		t.Errorf("request options: %v %v", option(req, optRequested), option(req, optServer))
	}
}

func TestAcquireAfterNakAndNoise(t *testing.T) {
	naks := 1
	s := &server{answer: func(typ byte, msg []byte) [][]byte {
		other := serverReply(dhcpOffer, msg, netdev.MAC{9, 9, 9, 9, 9, 9}) // for another card
		switch {
		case typ == dhcpRequest && naks > 0:
			naks--
			return [][]byte{[]byte("garbage"), serverReply(dhcpNak, msg, cardMAC)}
		case typ == dhcpDiscover:
			return [][]byte{other, serverReply(dhcpOffer, msg, cardMAC)}
		}
		return slirp(typ, msg)
	}}
	if _, err := Acquire(s, cardMAC, 7); err != nil {
		t.Fatal(err)
	}
	if len(s.sent) != 4 || binary.BigEndian.Uint32(s.sent[2][4:]) != 8 {
		t.Errorf("%d messages; the second try's xid %d", len(s.sent), binary.BigEndian.Uint32(s.sent[2][4:]))
	}
}

func TestNoServer(t *testing.T) {
	s := &server{answer: func(byte, []byte) [][]byte { return nil }}
	if _, err := Acquire(s, cardMAC, 1); !errors.Is(err, ErrNoLease) || len(s.sent) != dhcpTries {
		t.Errorf("%v after %d messages", err, len(s.sent))
	}
}

func TestRenew(t *testing.T) {
	s := &server{answer: slirp}
	l, _ := Acquire(s, cardMAC, 1)
	if _, err := Renew(s, cardMAC, 2, l); err != nil {
		t.Fatal(err)
	}
	req := s.sent[len(s.sent)-1]
	if binary.BigEndian.Uint16(req[10:]) != 0 || string(req[12:16]) != string([]byte{10, 0, 2, 15}) || option(req, optRequested) != nil {
		t.Errorf("renewal % x", req[:20])
	}
}

func TestParseRejects(t *testing.T) {
	good := serverReply(dhcpAck, dhcpMessage(dhcpRequest, 5, cardMAC, netip.Addr{}, netip.Addr{}, netip.Addr{}), cardMAC)
	bad := append([]byte(nil), good[:bootpBytes+4]...)
	bad = append(bad, optType, 9, 1) // the option says 9 bytes, 1 follows
	for name, b := range map[string][]byte{"short": good[:100], "overrun": bad, "no cookie": append(good[:bootpBytes:bootpBytes], 1, 2, 3, 4)} {
		if _, err := parseReply(b, cardMAC); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestFrames(t *testing.T) {
	msg := []byte("dhcp payload")
	f := broadcastFrame(cardMAC, msg)
	if ipChecksum(f[14:34]) != 0 || binary.BigEndian.Uint16(f[36:]) != 67 {
		t.Fatalf("frame % x", f[:42])
	}
	if _, ok := udpTo68(f); ok {
		t.Error("a frame to port 67 was taken as ours")
	}
	binary.BigEndian.PutUint16(f[36:], 68) // as a server's answer would be addressed
	if p, ok := udpTo68(f); !ok || string(p) != string(msg) {
		t.Errorf("udpTo68 = %q, %v", p, ok)
	}
}

// TestFrameLink: the first exchange as raw frames on a card, a server on
// the other end of the cable.
func TestFrameLink(t *testing.T) {
	card, far := cable()
	far.SetReceiver(func(f []byte) {
		msg := f[42:]
		mac := netdev.MAC(msg[28:34]) // answer the card that asked
		var r []byte
		switch msgType(msg) {
		case dhcpDiscover:
			r = serverReply(dhcpOffer, msg, mac)
		case dhcpRequest:
			r = serverReply(dhcpAck, msg, mac)
		}
		out := broadcastFrame(far.MAC(), r)
		binary.BigEndian.PutUint16(out[34:], 67)
		binary.BigEndian.PutUint16(out[36:], 68)
		far.Send(out)
	})
	link := FrameLink(card)
	defer link.Close()
	l, err := Acquire(link, card.MAC(), 99)
	if err != nil || l.Addr != netip.MustParsePrefix("10.0.2.15/24") {
		t.Fatalf("lease %+v, %v", l, err)
	}
}

func TestConfigDHCP(t *testing.T) {
	c, err := ParseConfig(strings.NewReader("eth0 dhcp # ask first\n"))
	if err != nil || !c.DHCP || c.Addr.IsValid() {
		t.Errorf("dhcp alone: %+v, %v", c, err)
	}
	if c, err = ParseConfig(strings.NewReader("eth0 dhcp\naddress 10.0.2.15/24\n")); err != nil || !c.DHCP || !c.Addr.IsValid() {
		t.Errorf("dhcp with a fallback: %+v, %v", c, err)
	}
	if _, err = ParseConfig(strings.NewReader("eth0 static\n")); err == nil {
		t.Error("eth0 static was accepted")
	}
}
