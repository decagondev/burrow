//go:build burrow

package main

// The network at boot (W17): pages for card DMA, a first check that eth0's
// link carries frames (an ARP for the gateway, before any stack exists),
// then the TCP/IP stack with /etc/net.conf's addresses and a ping of the
// gateway. Never waits long: no network is not an error.

import (
	"bytes"
	"io/fs"
	"net/netip"
	"time"

	"burrow/kernel/arch/amd64"
	"burrow/kernel/klog"
	"burrow/kernel/netdev"
	"burrow/kernel/netstack"
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
	if ok {
		checkLink(log.With("e1000"), nic, cfg)
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
	rtt, err := s.Ping(cfg.Gateway, pingWait)
	if err != nil {
		l.Warnf("%v", err)
		return
	}
	l.Infof("%v answers ping in %d ms", cfg.Gateway, rtt.Milliseconds())
}

// netConfig reads /etc/net.conf, or uses QEMU's addresses.
func (k *kernel) netConfig(l *klog.Logger) netstack.Config {
	data, err := fs.ReadFile(k.fs, netConf)
	if err != nil {
		return defaultNet
	}
	cfg, err := netstack.ParseConfig(bytes.NewReader(data))
	if err != nil {
		l.Warnf("%v; using %v", err, defaultNet.Addr)
		return defaultNet
	}
	return cfg
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
