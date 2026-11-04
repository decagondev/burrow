# ADR-0009: gVisor's TCP/IP stack in the kernel, vendored

- **Status:** Accepted
- **Date:** 2026-11-04
- **Wave:** W17

## Context
W17 puts Burrow on the network: user programs expect Linux sockets (`AF_INET`, `AF_INET6`) with TCP that survives loss and reordering, UDP, ICMP, ARP and IPv6 neighbour discovery. The Go kernel already has what a stack written in Go needs (goroutines, timers, channels), and its drivers hand over whole Ethernet frames (`kernel/netdev`, W17-S01). The wave has six slices; the stack is one of them.

## Options
1. **Write our own stack.** The most to learn, and the most to get wrong: TCP alone (retransmission, windows, congestion control, the state machine's corners) is a wave of its own, and every bug would surface as a user program hanging.
2. **gVisor's `pkg/tcpip`.** A complete stack in pure Go (IPv4, IPv6, ARP, ICMP, TCP with SACK and congestion control, UDP), tested in production and used as a library outside gVisor (Tailscale's userspace networking). Its `link/channel` and `link/ethernet` endpoints make the bridge to a card a page of code. Cost: a dependency on a large module (vendored, 41 of its packages plus x/sys, x/exp, x/time and google/btree), a bigger kernel, and code we did not write running in ring 0.
3. **lwIP or another C stack in the nucleus.** Mechanism in the nucleus, policy in Go is the rule (ADR-0001); a stack is policy, and C there would mean a second socket layer to bridge.

## Decision
We choose gVisor's stack (option 2), at a pinned version of its `go` branch (`v0.0.0-20261104092019-e0105ccf200a`), vendored into `kernel/vendor` so a build needs no network and the code we run is in the repository. `kernel/netstack` owns it: loopback and eth0, addresses from `/etc/net.conf`, routes, ping, and (W17-S03) the endpoints behind `AF_INET` sockets.

## Consequences
- Easier: TCP, UDP and ICMP behave like Linux's from the start; the slices go into the socket calls, names and servers instead.
- Harder: the kernel binary grows by a few MB; an update of the stack is a deliberate, reviewed change (`go get`, `go mod vendor`, the tests and the census). The site and `go vet ./...` must skip `vendor/`.
- To watch: the stack's goroutines and timers share Go's single P with the syscall dispatcher (BUG-0014, PIT-47); any system call it adds to the Go runtime's census (`make census`); and its memory use under load.
