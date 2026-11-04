package netstack

import (
	"bytes"
	"io"
	"net/netip"
	"strings"
	"sync"
	"testing"
	"time"

	"gvisor.dev/gvisor/pkg/tcpip"
	"gvisor.dev/gvisor/pkg/tcpip/adapters/gonet"
	"gvisor.dev/gvisor/pkg/tcpip/network/ipv4"

	"burrow/kernel/netdev"
)

// wire is two fake cards joined by a cable: what one sends, the other
// receives, on its own goroutine as from an interrupt.
type wire struct {
	mu   sync.Mutex
	mac  netdev.MAC
	peer *wire
	recv func([]byte)
	sent int
}

func cable() (*wire, *wire) {
	a := &wire{mac: netdev.MAC{2, 0, 0, 0, 0, 1}}
	b := &wire{mac: netdev.MAC{2, 0, 0, 0, 0, 2}}
	a.peer, b.peer = b, a
	return a, b
}

func (w *wire) MAC() netdev.MAC     { return w.mac }
func (w *wire) MTU() int            { return 1500 }
func (w *wire) LinkUp() bool        { return true }
func (w *wire) Stats() netdev.Stats { return netdev.Stats{TxFrames: uint64(w.sent)} }
func (w *wire) SetReceiver(f func([]byte)) {
	w.mu.Lock()
	w.recv = f
	w.mu.Unlock()
}

func (w *wire) Send(frame []byte) error {
	w.mu.Lock()
	w.sent++
	w.mu.Unlock()
	w.peer.mu.Lock()
	recv := w.peer.recv
	w.peer.mu.Unlock()
	if recv != nil {
		f := append([]byte(nil), frame...) // the sender reuses its buffer
		go recv(f)
	}
	return nil
}

func pair(t *testing.T) (*Stack, *Stack) {
	t.Helper()
	a, b := cable()
	sa, err := New(a, Config{Addr: netip.MustParsePrefix("10.0.2.15/24"), Gateway: netip.MustParseAddr("10.0.2.2")})
	if err != nil {
		t.Fatal(err)
	}
	sb, err := New(b, Config{Addr: netip.MustParsePrefix("10.0.2.2/24")})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { sa.Close(); sb.Close() })
	return sa, sb
}

func TestPingAcrossTheCable(t *testing.T) {
	a, _ := pair(t)
	rtt, err := a.Ping(netip.MustParseAddr("10.0.2.2"), 2*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if rtt <= 0 || rtt > time.Second {
		t.Fatalf("round trip %v", rtt)
	}
	if _, err := a.Ping(netip.MustParseAddr("10.0.2.9"), 300*time.Millisecond); err == nil {
		t.Fatal("an address nobody has answered")
	}
}

func TestTCPHandshakeAndEcho(t *testing.T) {
	a, b := pair(t)
	ln, err := gonet.ListenTCP(b.Stack, tcpip.FullAddress{Port: 8000}, ipv4.ProtocolNumber)
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		c, err := ln.Accept()
		if err != nil {
			return
		}
		io.Copy(c, c)
		c.Close()
	}()
	c, err := gonet.DialTCP(a.Stack, tcpip.FullAddress{Addr: Addr(netip.MustParseAddr("10.0.2.2")), Port: 8000}, ipv4.ProtocolNumber)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	msg := bytes.Repeat([]byte("burrow over tcp "), 1000) // several segments
	go c.Write(msg)
	got := make([]byte, len(msg))
	c.SetReadDeadline(time.Now().Add(5 * time.Second))
	if _, err := io.ReadFull(c, got); err != nil || !bytes.Equal(got, msg) {
		t.Fatalf("echo: %v", err)
	}
}

func TestLoopback(t *testing.T) {
	s, err := New(nil, Config{})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if _, err := s.Ping(netip.MustParseAddr("127.0.0.1"), time.Second); err != nil {
		t.Fatal(err)
	}
	var out strings.Builder
	s.Show(&out)
	if !strings.Contains(out.String(), "lo: mtu") || !strings.Contains(out.String(), "127.0.0.1/8") {
		t.Fatalf("Show:\n%s", out.String())
	}
}

func TestShowEth0(t *testing.T) {
	a, _ := pair(t)
	a.Ping(netip.MustParseAddr("10.0.2.2"), time.Second)
	var out strings.Builder
	a.Show(&out)
	for _, want := range []string{"eth0: mtu 1500 ether 02:00:00:00:00:01", "inet 10.0.2.15/24", "route 0.0.0.0/0 via 10.0.2.2 dev eth0", "card: rx"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("Show has no %q:\n%s", want, out.String())
		}
	}
}

func TestParseConfig(t *testing.T) {
	c, err := ParseConfig(strings.NewReader("# eth0\naddress 10.0.2.15/24\ngateway 10.0.2.2 # slirp\ndns 10.0.2.3\n"))
	if err != nil || c.Addr.String() != "10.0.2.15/24" || c.Gateway.String() != "10.0.2.2" || len(c.DNS) != 1 {
		t.Fatalf("%+v, %v", c, err)
	}
	for _, bad := range []string{"", "address 10.0.2.15\n", "address 10.0.2.15/24\ngateway 10.0.3.2\n", "speed 100\n", "address 1 2\n"} {
		if _, err := ParseConfig(strings.NewReader(bad)); err == nil {
			t.Errorf("%q was accepted", bad)
		}
	}
}
