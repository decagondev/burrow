//go:build burrow

package main

// The network at boot (W17): pages for card DMA, and a first check that a
// card is there and its link carries frames, before any stack exists.
// Never waits long: no network is not an error.

import (
	"net/netip"
	"time"

	"burrow/kernel/arch/amd64"
	"burrow/kernel/klog"
	"burrow/kernel/netdev"
)

// QEMU's user network (slirp): Burrow is 10.0.2.15, the gateway 10.0.2.2.
var (
	bootAddr = netip.MustParseAddr("10.0.2.15")
	gateway  = netip.MustParseAddr("10.0.2.2")
)

const (
	arpTries = 3
	arpWait  = 500 * time.Millisecond
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

// checkNetwork asks eth0's network who the gateway is (W17-S01): the
// answer proves frames go out and come back through the driver.
func checkNetwork(log *klog.Logger) {
	nic, ok := netdev.Get("eth0")
	if !ok {
		return
	}
	l := log.With("e1000")
	mac, err := netdev.Resolve(nic, bootAddr, gateway, arpTries, arpWait)
	if err != nil {
		l.Warnf("ARP: %v", err)
		return
	}
	l.Infof("%v is at %v", gateway, mac)
}
