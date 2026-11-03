# W17 Networking  (M17)  — status: planned

Goal: Burrow on the network: a Go driver for an Intel e1000 card, a TCP/IP stack in the Go kernel, and Linux sockets (`AF_INET`, `AF_INET6`) for user programs, so busybox `wget`, a Go program's `net/http` and a small web server work. Demo: `make run-net` boots with QEMU's user-mode network; `wget -O- http://10.0.2.2:8000/` fetches a page the host serves; `run /bin/gofetch https://example.com` prints a page fetched over TLS (Go's own crypto, in the user program); busybox `httpd` inside Burrow serves `/home` to a browser on the host through a forwarded port. Stories: E17 ([BACKLOG.md](../BACKLOG.md)).

Where we start (after W16): PCI enumeration and MMIO mapping for drivers (`driver.Env.MapMMIO`, W8), IRQs as goroutine wake-ups (W7), frames for DMA from `nu_page_alloc` with their physical addresses (W9), the socket layer with readiness for `poll`/`select`/epoll and non-blocking I/O (W14-S06, S07), `getrandom` and a real clock (W14), and the Go kernel's own runtime, which already has everything a pure-Go network stack wants (goroutines, timers, channels).

Design (mechanism in the nucleus, policy in Go, as ever):
- **The card: e1000.** QEMU's `-nic user,model=e1000` (82540EM) is the simplest common NIC: MMIO registers in BAR 0, one receive and one transmit descriptor ring, legacy INTx. `kernel/drivers/e1000` resets it, reads the MAC from the EEPROM, sets up the rings in frames from `nu_page_alloc` (a 4 KiB page holds 256 descriptors; each buffer is 2 KiB, two to a page, so no contiguous DMA allocation is needed), and serves receive and transmit-done interrupts on its IRQ goroutine. virtio-net is the faster option for later (E12).
- **The stack: gVisor's `tcpip`.** `gvisor.dev/gvisor/pkg/tcpip` is a complete TCP/IP stack in pure Go (IPv4, IPv6, ARP, ICMP, TCP with congestion control, UDP), used as a library outside gVisor (Tailscale's userspace networking runs on it). The kernel imports it at a pinned version (a module download, a go-ahead first); a link endpoint bridges it to the e1000 driver. Writing a stack ourselves is a wave of its own and would teach less than it costs. The kernel binary grows by a few MB; `make census` must stay green.
- **Addresses without DHCP at first.** QEMU's user network is fixed: Burrow is 10.0.2.15/24, the gateway (and the host) 10.0.2.2, DNS 10.0.2.3. A boot-time `net.conf` on the initrd sets these; DHCP (gVisor ships a client) can replace it later. Loopback (`127.0.0.1`, `::1`) comes with the stack.
- **Sockets are files.** An `AF_INET` socket is a `proc` file wrapping a gVisor endpoint, a `Canceller` and a `Poller` like the Unix sockets of W14, so `read`, `write`, `poll`, `select`, epoll, `O_NONBLOCK` and `EINTR` come for free; `connect`, `bind`, `listen`, `accept4`, `sendto`/`recvfrom`/`sendmsg`/`recvmsg`, `shutdown`, `getsockname`/`getpeername`, `setsockopt`/`getsockopt` (`SO_REUSEADDR`, `SO_KEEPALIVE`, `SO_ERROR`, `SO_RCVBUF`, `SO_SNDBUF`, `TCP_NODELAY`, `IPV6_V6ONLY`) map onto it. Names stay in user space: musl and Go resolve through `/etc/resolv.conf`, `/etc/hosts` and UDP DNS.
- **Tests without the internet.** Every smoke test talks to the host only: QEMU's user network reaches the host at 10.0.2.2, where the test starts a small Python HTTP server, and `hostfwd` lets the host reach a server inside Burrow. The HTTPS demo needs the internet and is only in `make run-net`.

### [ ] W17-S01 An e1000 driver (E17)
`kernel/drivers/e1000`: probe by PCI ID (8086:100E), reset, the MAC address, receive and transmit rings in frames from the nucleus, interrupts as wake-ups, link status. A host test against a fake register file and fake DMA memory (`haltest`), checking ring handling and wrap-around; on QEMU the driver sends a raw ARP request for 10.0.2.2 and logs the reply (`[e1000] INFO: 10.0.2.2 is at 52:55:0a:00:02:02`, a smoke marker). Needs from the nucleus: physical addresses of frames Go allocated for a driver, and the PCI device's interrupt line through the 8259 (as the disks' and the PS/2's).

### [ ] W17-S02 TCP/IP in the kernel (E17)
gVisor's stack, pinned and vendored, wired to the driver through a link endpoint; static addresses from `/etc/net.conf`; the kernel pings the gateway at boot (`[net] INFO: 10.0.2.2 answers ping in N ms`) and a console command `net` shows addresses, routes and counters. Host tests: the link endpoint against a fake driver, a TCP handshake through two stacks joined back to back. The kernel binary's size and the Go runtime census are checked again.

### [ ] W17-S03 Sockets for user programs (E17)
`socket(AF_INET|AF_INET6, SOCK_STREAM|SOCK_DGRAM)` and the calls above as files over gVisor endpoints, with Linux's error numbers (`ECONNREFUSED`, `EINPROGRESS` and `SO_ERROR` for a non-blocking connect, `EADDRINUSE`, `ETIMEDOUT`). A `nettest` program (TCP and UDP over loopback, a non-blocking connect, `poll` readiness, `shutdown`, a refused port) runs on the host's Linux and on Burrow, like `socktest`.

### [ ] W17-S04 Names and the outside (E17)
`/etc/resolv.conf`, `/etc/hosts`, `/etc/services` and `/etc/protocols` on the initrd; `uname`'s node name and `sethostname`; musl's `getaddrinfo` and busybox `nslookup`, `wget` and `ping` (ICMP over `SOCK_DGRAM`/`IPPROTO_ICMP`, or raw sockets for root). `make net-smoke` (in `make test`): the test starts `python3 -m http.server` on the host, and Burrow's `wget` fetches from it through 10.0.2.2.

### [ ] W17-S05 Servers inside Burrow (E17)
`listen`/`accept` under load (busybox `httpd` serving `/home`, many short connections), `SO_REUSEADDR` restarts, and QEMU `hostfwd` so the host's `curl` reaches it; the smoke test fetches a file from Burrow and compares it. A Go user program (`user/go/goserve`) serves HTTP with `net/http` too, which exercises epoll and goroutines over sockets.

### [ ] W17-S06 make run-net, gofetch, and the networking guide (E17)
`user/go/gofetch` (Go's `net/http` and `crypto/tls`: DNS through `/etc/resolv.conf`, a TLS handshake, the page printed), `make run-net` with user networking and a forwarded port, throughput budgets in `make net-bench` (not in `make test`: a 10 MB download from the host, connections per second to `httpd`), and `docs/06-guides/NETWORKING.md`: addresses, what is supported, and how to reach the host and the internet from QEMU.

Exit: **M17 demo — on the network.** `make net-smoke` in `make test` (no internet needed); `make clean test` green; POSIX_COVERAGE.md covers every socket call for `AF_INET`/`AF_INET6`; DEVLOG wave entry.

Risks and pitfalls (PIT-NN entries go into BUGS_AND_MITIGATIONS.md when the wave starts):
- **DMA addresses.** The card must be given physical addresses of frames that stay put; Go memory is virtual and demand-zero (BUG-0012). Descriptor rings and buffers come from `nu_page_alloc` frames (physical, never moved), never from Go's heap.
- **Interrupts and the 8259.** QEMU's e1000 uses a PCI INTx line routed to an ISA IRQ (often 11, shared); the line number comes from PCI config space, and a shared line means asking the card whether it interrupted.
- **gVisor's dependencies and size.** The module pulls in more of gVisor than the stack; vendoring a pinned subset and watching the kernel's size and the runtime census keeps it honest. It needs a Go version at least as new as its `go.mod` says; the kernel's toolchain (1.27.3) is newer than any it asks for so far.
- **Two schedulers.** gVisor's stack makes goroutines and timers for TCP; in a kernel where the Go runtime has one P (BUG-0014), a busy socket must not starve the syscall dispatcher. S03 measures it.
- **Tests and the internet.** CI has no network inside QEMU's slirp beyond the host; every check in `make test` talks to the host only.
