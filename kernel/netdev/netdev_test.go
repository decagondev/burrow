package netdev

import (
	"net/netip"
	"slices"
	"strconv"
	"sync"
	"testing"
	"time"
)

// loopCard answers ARP requests for one address, like QEMU's user network.
type loopCard struct {
	mu     sync.Mutex
	recv   func([]byte)
	answer netip.Addr
	sent   int
}

var gateway = MAC{0x52, 0x55, 0x0a, 0x00, 0x02, 0x02}

func (c *loopCard) MAC() MAC     { return MAC{0x52, 0x54, 0, 0x12, 0x34, 0x56} }
func (c *loopCard) MTU() int     { return 1500 }
func (c *loopCard) LinkUp() bool { return true }
func (c *loopCard) Stats() Stats { return Stats{} }
func (c *loopCard) SetReceiver(f func([]byte)) {
	c.mu.Lock()
	c.recv = f
	c.mu.Unlock()
}

func (c *loopCard) Send(f []byte) error {
	c.mu.Lock()
	c.sent++
	recv, answer := c.recv, c.sent > 1 // ignore the first request: test the retry
	c.mu.Unlock()
	if !answer || recv == nil {
		return nil
	}
	reply := append([]byte(nil), f...)
	copy(reply[0:6], f[6:12])
	copy(reply[6:12], gateway[:])
	a := reply[ethHeader:]
	a[7] = arpReply
	copy(a[18:24], f[ethHeader+8:ethHeader+14]) // target = the asker
	copy(a[24:28], f[ethHeader+14:ethHeader+18])
	copy(a[8:14], gateway[:]) // sender = the gateway
	t := c.answer.As4()
	copy(a[14:18], t[:])
	go recv(reply)
	return nil
}

func TestARPRequestLayout(t *testing.T) {
	src, dst := netip.MustParseAddr("10.0.2.15"), netip.MustParseAddr("10.0.2.2")
	f := ARPRequest(MAC{1, 2, 3, 4, 5, 6}, src, dst)
	if len(f) != minFrame || MAC(f[0:6]) != Broadcast || f[12] != 0x08 || f[13] != 0x06 {
		t.Fatalf("header % x", f[:14])
	}
	a := f[ethHeader:]
	if a[7] != arpRequest || netip.AddrFrom4([4]byte(a[14:18])) != src || netip.AddrFrom4([4]byte(a[24:28])) != dst {
		t.Fatalf("body % x", a[:arpBody])
	}
	if _, ok := ARPReplyFrom(f, dst); ok {
		t.Fatal("a request read as a reply")
	}
}

func TestResolveRetriesAndReadsTheReply(t *testing.T) {
	gw := netip.MustParseAddr("10.0.2.2")
	c := &loopCard{answer: gw}
	mac, err := Resolve(c, netip.MustParseAddr("10.0.2.15"), gw, 3, 200*time.Millisecond)
	if err != nil || mac != gateway {
		t.Fatalf("Resolve = %v, %v; want %v", mac, err, gateway)
	}
	if c.sent != 2 || c.recv != nil {
		t.Fatalf("sent %d requests, receiver left %v", c.sent, c.recv != nil)
	}
	if _, err := Resolve(c, netip.MustParseAddr("10.0.2.15"), netip.MustParseAddr("10.0.2.9"), 2, 20*time.Millisecond); err == nil {
		t.Fatal("an address nobody has resolved")
	}
}

var registered int // TestRegistry may run more than once (-count)

func TestRegistry(t *testing.T) {
	registered++
	name := "test" + strconv.Itoa(registered)
	Register(name, &loopCard{})
	if d, ok := Get(name); !ok || d == nil || !slices.Contains(Names(), name) {
		t.Fatalf("%s not found", name)
	}
	defer func() {
		if recover() == nil {
			t.Fatalf("a second %s was accepted", name)
		}
	}()
	Register(name, &loopCard{})
}
