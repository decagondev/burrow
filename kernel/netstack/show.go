package netstack

import (
	"fmt"
	"io"
	"sort"
	"time"

	"gvisor.dev/gvisor/pkg/tcpip"
)

// Show writes what the console's `net` command prints: each interface with
// its addresses and counters, the routes, and the card's own counters.
func (s *Stack) Show(w io.Writer) {
	infos := s.NICInfo()
	ids := make([]int, 0, len(infos))
	for id := range infos {
		ids = append(ids, int(id))
	}
	sort.Ints(ids)
	for _, id := range ids {
		info := infos[tcpip.NICID(id)]
		fmt.Fprintf(w, "%s: mtu %d", info.Name, info.MTU)
		if len(info.LinkAddress) == 6 {
			fmt.Fprintf(w, " ether %v", info.LinkAddress)
		}
		fmt.Fprintln(w)
		for _, a := range info.ProtocolAddresses {
			fmt.Fprintf(w, "    inet %v\n", a.AddressWithPrefix)
		}
		st := info.Stats
		fmt.Fprintf(w, "    rx %d packets %d bytes, tx %d packets %d bytes\n",
			st.Rx.Packets.Value(), st.Rx.Bytes.Value(), st.Tx.Packets.Value(), st.Tx.Bytes.Value())
	}
	for _, r := range s.GetRouteTable() {
		via := "direct"
		if r.Gateway.Len() > 0 {
			via = "via " + r.Gateway.String()
		}
		fmt.Fprintf(w, "route %v %s dev %s\n", r.Destination, via, infos[r.NIC].Name)
	}
	if l, got, ok := s.lease.get(); ok {
		left := time.Until(got.Add(l.Renew)).Round(time.Second)
		fmt.Fprintf(w, "dhcp: %v from %v, lease %v, renew in %v\n", l.Addr, l.Server, l.Time, left)
	}
	if s.dev != nil {
		c := s.dev.Stats()
		fmt.Fprintf(w, "card: rx %d frames (%d dropped), tx %d frames (%d busy)\n", c.RxFrames, c.RxDropped, c.TxFrames, c.TxBusy)
	}
	ip := s.Stats().IP
	fmt.Fprintf(w, "ip: %d received, %d delivered, %d sent, %d malformed\n", ip.PacketsReceived.Value(),
		ip.PacketsDelivered.Value(), ip.PacketsSent.Value(), ip.MalformedPacketsReceived.Value())
}
