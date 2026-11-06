//go:build burrow

package main

// The network at boot (W17): pages for card DMA, a lease from a DHCP server
// when net.conf or the kargs (net=dhcp) ask for one (W23-S05), a first
// check that eth0's link carries frames (an ARP for the gateway, before any
// stack exists), then the TCP/IP stack with the addresses and a ping of
// the gateway. Never waits long: no network (or no lease) is not an error.

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io/fs"
	"net/netip"
	"os"
	"strings"
	"time"

	"burrow/kernel/arch/amd64"
	"burrow/kernel/klog"
	"burrow/kernel/netdev"
	"burrow/kernel/netstack"
	"burrow/kernel/random"
)

// QEMU's user network (slirp), when /etc/net.conf is missing: Burrow is
// 10.0.2.15, the gateway 10.0.2.2.
var defaultNet = netstack.Config{
	Addr:    netip.MustParsePrefix("10.0.2.15/24"),
	Gateway: netip.MustParseAddr("10.0.2.2"),
	DNS:     []netip.Addr{netip.MustParseAddr("10.0.2.3")},
}

const (
	arpTries = 3
	arpWait  = 500 * time.Millisecond
	pingWait = 2 * time.Second
	netConf  = "etc/net.conf"
)

// dmaPage is driver.Env.DMAPage: a frame from the nucleus (zeroed,
// physical, never moved), seen through the physmap.
func dmaPage() (uint64, []byte, error) {
	pa, err := amd64.Nucleus{}.PageAlloc()
	if err != nil {
		return 0, nil, err
	}
	return pa, amd64.PhysBytes(pa, 4096), nil
}

// startNetwork checks eth0's link (W17-S01), then starts the stack on it
// (W17-S02); with no card the stack has loopback only.
func (k *kernel) startNetwork(log *klog.Logger) {
	cfg := k.netConfig(log.With("net"))
	nic, ok := netdev.Get("eth0") // nic is nil without a card
	var lease *netstack.Lease
	if ok && cfg.DHCP {
		cfg, lease = k.dhcp(log.With("net"), nic, cfg)
	}
	if ok {
		checkLink(log.With(driverOf(nic)), nic, cfg)
	}
	s, err := netstack.New(nic, cfg)
	if err != nil {
		log.With("net").Errorf("%v", err)
		return
	}
	k.net = s
	l := log.With("net")
	if nic == nil {
		l.Infof("no card: loopback only")
		return
	}
	l.Infof("eth0 %v, gateway %v", cfg.Addr, cfg.Gateway)
	if lease != nil {
		s.KeepLease(*lease, randomXID(), func(x netstack.Lease, err error) {
			if err != nil {
				l.Warnf("dhcp renewal: %v", err)
				return
			}
			l.Infof("dhcp: %v renewed for %v", x.Addr, x.Time)
		})
	}
	rtt, err := s.Ping(cfg.Gateway, pingWait)
	if err != nil {
		l.Warnf("%v", err)
		return
	}
	l.Infof("%v answers ping in %d ms", cfg.Gateway, rtt.Milliseconds())
}

// netConfig reads /etc/net.conf, or uses QEMU's addresses; the kargs word
// net=dhcp asks for DHCP whatever the file says.
func (k *kernel) netConfig(l *klog.Logger) netstack.Config {
	cfg := defaultNet
	if data, err := fs.ReadFile(k.fs, netConf); err == nil {
		if cfg, err = netstack.ParseConfig(bytes.NewReader(data)); err != nil {
			l.Warnf("%v; using %v", err, defaultNet.Addr)
			cfg = defaultNet
		}
	}
	for _, a := range os.Args[1:] {
		if a == "net=dhcp" {
			cfg.DHCP = true
		}
	}
	return cfg
}

// dhcp asks for a lease on raw frames (the stack does not exist yet); with
// none, the fixed addresses (or QEMU's) are used.
func (k *kernel) dhcp(l *klog.Logger, nic netdev.Device, cfg netstack.Config) (netstack.Config, *netstack.Lease) {
	link := netstack.FrameLink(nic)
	lease, err := netstack.Acquire(link, nic.MAC(), randomXID())
	link.Close()
	if err != nil {
		if !cfg.Addr.IsValid() {
			cfg.Addr, cfg.Gateway, cfg.DNS = defaultNet.Addr, defaultNet.Gateway, defaultNet.DNS
		}
		l.Warnf("dhcp: %v; using %v", err, cfg.Addr)
		return cfg, nil
	}
	cfg.Addr, cfg.Gateway, cfg.DNS = lease.Addr, lease.Gateway, lease.DNS
	l.Infof("eth0 dhcp %v via %v, lease %v from %v", lease.Addr, lease.Gateway, lease.Time, lease.Server)
	k.writeResolv(l, lease.DNS)
	return cfg, &lease
}

// writeResolv puts the lease's DNS servers in /etc/resolv.conf when /etc
// can be written (an ext2 root); the initrd's stays otherwise.
func (k *kernel) writeResolv(l *klog.Logger, dns []netip.Addr) {
	if len(dns) == 0 {
		return
	}
	var b strings.Builder
	b.WriteString("# from the DHCP lease (W23-S05)\n")
	for _, a := range dns {
		fmt.Fprintf(&b, "nameserver %v\n", a)
	}
	if err := k.fs.WriteFile("etc/resolv.conf", []byte(b.String())); err != nil {
		l.Infof("resolv.conf kept (%v)", err)
	}
}

// randomXID is a DHCP transaction id.
func randomXID() uint32 {
	var b [4]byte
	random.Kernel.Read(b[:])
	return binary.LittleEndian.Uint32(b[:])
}

// driverOf names the card's driver for the log (e1000 unless it says).
func driverOf(nic netdev.Device) string {
	if d, ok := nic.(interface{ Driver() string }); ok {
		return d.Driver()
	}
	return "e1000"
}

// checkLink asks eth0's network who the gateway is: the answer proves
// frames go out and come back through the driver.
func checkLink(l *klog.Logger, nic netdev.Device, cfg netstack.Config) {
	mac, err := netdev.Resolve(nic, cfg.Addr.Addr(), cfg.Gateway, arpTries, arpWait)
	if err != nil {
		l.Warnf("ARP: %v", err)
		return
	}
	l.Infof("%v is at %v", cfg.Gateway, mac)
}
