package netstack

import (
	"bytes"
	"errors"
	"fmt"
	"net/netip"
	"strings"
	"time"

	"gvisor.dev/gvisor/pkg/tcpip"
	"gvisor.dev/gvisor/pkg/tcpip/header"
	"gvisor.dev/gvisor/pkg/tcpip/network/ipv4"
	"gvisor.dev/gvisor/pkg/tcpip/transport/icmp"
	"gvisor.dev/gvisor/pkg/waiter"
)

const pingPayload = "burrow ping"

// Ping sends one ICMP echo request to an IPv4 address and waits up to
// timeout for the reply; it returns the round trip.
func (s *Stack) Ping(to netip.Addr, timeout time.Duration) (time.Duration, error) {
	if !to.Is4() {
		return 0, fmt.Errorf("ping: %v is not IPv4", to)
	}
	var wq waiter.Queue
	ep, terr := s.NewEndpoint(icmp.ProtocolNumber4, ipv4.ProtocolNumber, &wq)
	if terr != nil {
		return 0, fmt.Errorf("ping: %v", terr)
	}
	defer ep.Close()
	entry, ready := waiter.NewChannelEntry(waiter.ReadableEvents)
	wq.EventRegister(&entry)
	defer wq.EventUnregister(&entry)

	msg := header.ICMPv4(make([]byte, header.ICMPv4MinimumSize+len(pingPayload)))
	msg.SetType(header.ICMPv4Echo)
	copy(msg.Payload(), pingPayload)
	start := time.Now()
	var r bytes.Reader
	r.Reset(msg)
	if _, terr := ep.Write(&r, tcpip.WriteOptions{To: &tcpip.FullAddress{Addr: Addr(to)}}); terr != nil {
		return 0, fmt.Errorf("ping %v: %v", to, terr)
	}
	deadline := time.After(timeout)
	for {
		var got bytes.Buffer
		_, terr := ep.Read(&got, tcpip.ReadOptions{})
		if terr == nil {
			reply := header.ICMPv4(got.Bytes())
			if len(reply) >= header.ICMPv4MinimumSize && reply.Type() == header.ICMPv4EchoReply &&
				strings.HasPrefix(string(reply.Payload()), pingPayload) {
				return time.Since(start), nil
			}
			continue
		}
		if _, wouldBlock := terr.(*tcpip.ErrWouldBlock); !wouldBlock {
			return 0, fmt.Errorf("ping %v: %v", to, terr)
		}
		select {
		case <-ready:
		case <-deadline:
			return 0, errors.New("ping " + to.String() + ": no reply")
		}
	}
}
