package netstack

import (
	"bufio"
	"fmt"
	"io"
	"net/netip"
	"strings"
)

// Config is eth0's addresses, read from /etc/net.conf: one setting a line,
//
//	address 10.0.2.15/24   (eth0's address and its subnet)
//	gateway 10.0.2.2       (where everything else goes)
//	dns     10.0.2.3       (for /etc/resolv.conf's readers; may repeat)
//	eth0 dhcp              (ask a DHCP server first, W23-S05; the lines
//	                        above, if any, are what to use without one)
//
// with # starting a comment.
type Config struct {
	Addr    netip.Prefix
	Gateway netip.Addr
	DNS     []netip.Addr
	DHCP    bool
}

// ParseConfig reads a net.conf.
func ParseConfig(r io.Reader) (Config, error) {
	var c Config
	sc := bufio.NewScanner(r)
	for n := 1; sc.Scan(); n++ {
		line, _, _ := strings.Cut(sc.Text(), "#")
		f := strings.Fields(line)
		if len(f) == 0 {
			continue
		}
		if len(f) != 2 {
			return c, fmt.Errorf("net.conf:%d: want a setting and one value", n)
		}
		var err error
		switch f[0] {
		case "eth0":
			if f[1] != "dhcp" {
				err = fmt.Errorf("eth0 %q: only dhcp", f[1])
			}
			c.DHCP = true
		case "address":
			c.Addr, err = netip.ParsePrefix(f[1])
		case "gateway":
			c.Gateway, err = netip.ParseAddr(f[1])
		case "dns":
			var a netip.Addr
			if a, err = netip.ParseAddr(f[1]); err == nil {
				c.DNS = append(c.DNS, a)
			}
		default:
			err = fmt.Errorf("unknown setting %q", f[0])
		}
		if err != nil {
			return c, fmt.Errorf("net.conf:%d: %v", n, err)
		}
	}
	if err := sc.Err(); err != nil {
		return c, err
	}
	if c.DHCP && !c.Addr.IsValid() {
		return c, nil // the lease will say
	}
	if !c.Addr.IsValid() || !c.Addr.Addr().Is4() {
		return c, fmt.Errorf("net.conf: no IPv4 address")
	}
	if c.Gateway.IsValid() && !c.Addr.Contains(c.Gateway) {
		return c, fmt.Errorf("net.conf: gateway %v is not in %v", c.Gateway, c.Addr)
	}
	return c, nil
}
