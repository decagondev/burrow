# Burrow on the network

Burrow has a network (W17): a Go driver for QEMU's Intel e1000 card, gVisor's TCP/IP stack inside the Go kernel ([ADR-0009](../03-decisions/ADR-0009-gvisor-tcpip.md)), and Linux sockets for user programs, so busybox `wget`, `ping` and `httpd`, Go's `net/http` and C programs written for Linux work unchanged. This guide is how to use it; [WORKBENCH.md](WORKBENCH.md) is the workbench underneath and [POSIX_COVERAGE.md](../02-architecture/POSIX_COVERAGE.md) lists every socket call.

## Start it

From the repository, in WSL or Linux:

```sh
make busybox ports      # once: busybox (wget, ping, httpd, nslookup, nc) and the workbench's tools
make run-net            # the workbench in a QEMU window, on QEMU's user network
```

`make run-net` is `make run-dev` with the network spelled out and the host's port 8080 forwarded into Burrow (`RUN_NET_FWD=9090 make run-net` picks another host port). Every Burrow image has the card, though: QEMU gives each machine an e1000 on its user network unless told otherwise, so `make run-go` and the others are on the network too, just without a forwarded port.

## Addresses

QEMU's user network is a private, fixed one, and Burrow's addresses come from `/etc/net.conf` on the initrd:

| Address | What it is |
|---|---|
| 10.0.2.15/24 | Burrow (`eth0`), and `burrow` in `/etc/hosts` |
| 10.0.2.2 | the gateway, which is also the host: a server on the host's 127.0.0.1 answers here; `host` in `/etc/hosts` |
| 10.0.2.3 | DNS: QEMU forwards each query to the host's resolver (`/etc/resolv.conf`) |
| 127.0.0.1, ::1 | loopback inside Burrow |

IPv6 works on loopback and between programs inside Burrow; `eth0` is configured for IPv4 only. There is no DHCP: the addresses never change. At boot the kernel asks for the gateway by ARP and pings it (`[e1000] INFO: 10.0.2.2 is at ...`, `[net] INFO: 10.0.2.2 answers ping in N ms`); the console command `net` shows the interfaces, addresses, routes and counters.

## Reaching the host and the internet

```text
run /bin/busybox sh
wget -O - http://host:8000/              a server on the host (python3 -m http.server 8000 there)
ping -c 3 10.0.2.2                       the gateway answers (pings beyond it depend on the host)
nslookup example.com                     names through QEMU's DNS
/bin/gofetch https://example.com/        HTTPS with Go's crypto, checked against Mozilla's roots
```

The internet is reachable when the host is: QEMU relays Burrow's TCP and UDP connections through the host's own sockets. `gofetch` (`user/go/gofetch`) carries its certificate roots inside the binary (`golang.org/x/crypto/x509roots/fallback`), because Burrow has no CA store; `-k` skips the check, `-o file` saves the body, `-stats` reports the rate. busybox `wget` speaks HTTP only (no TLS for C programs yet).

## Serving from Burrow

A server inside Burrow is reached from the host through the forwarded port:

```text
httpd -f -p 8080 -h /home                busybox httpd serves your home disk
/bin/goserve -addr :8080 -dir /home      or Go's net/http does
```

then open `http://localhost:8080/` on the host. Only forwarded ports are reachable from outside: QEMU's user network does not route to Burrow otherwise. A server can be stopped and started again on the same port at once (`SO_REUSEADDR`, which busybox httpd and Go set).

## What is supported

- **Sockets:** `AF_INET` and `AF_INET6`, TCP (`SOCK_STREAM`) and UDP (`SOCK_DGRAM`), raw ICMP for `ping`; blocking and non-blocking, `poll`, `select` and epoll; `connect` with `EINPROGRESS` and `SO_ERROR`; the common options (`SO_REUSEADDR`, `SO_KEEPALIVE`, the buffer sizes, `TCP_NODELAY`, the keepalive timers, `IPV6_V6ONLY`). Errors are Linux's numbers.
- **Names:** `/etc/hosts`, `/etc/resolv.conf`, `/etc/services`, `/etc/protocols`; musl and Go read them as on Linux. `sethostname` changes what `uname -n` says.
- **Speed** (QEMU without KVM; `make net-bench`): about 5.5 MB/s for a 10 MB download from the host, and a dozen short connections a second to busybox httpd, which forks for each; loopback TCP moves about 35 MB/s between two processes.
- **Not yet:** DHCP, virtio-net, packet sockets and raw sockets beyond ICMP, `SIOC*` interface ioctls (so no `ifconfig`; use the console's `net`), TLS for C programs, a firewall. Copy-on-write fork (E18) would speed up forking servers.

## How it is tested

`make net-smoke` (in `make test`) needs nothing beyond the host: Burrow's busybox and `gofetch` fetch from Python servers on the host (plain HTTP and TLS with a throwaway certificate), `ping` the gateway, and the host fetches from busybox httpd and `goserve` inside Burrow through forwarded ports (`tests/smoke/net-smoke.sh`, `net-serve.sh`, `fetch.py`). `make ports-smoke` runs `user/nettest`, the socket checks, on the host's Linux first and then on Burrow; `make net-bench` checks the speed against budgets. The internet is only ever needed by hand, in `make run-net`.

## virtio-net and DHCP (W23-S05)

Burrow also drives QEMU's virtio-net card (`drivers/virtionet`): `make run-dev NIC=virtio` (and the other run targets, `run-net` included) attach it instead of the e1000, and `eth0` is the same to everything above the driver. It is the faster card under QEMU: `make net-bench` downloads 10 MB at about 12 MB/s on virtio-net against 5 on the e1000.

Addresses can come from DHCP: put `eth0 dhcp` in `/etc/net.conf` (any `address`, `gateway` and `dns` lines stay as the fallback), or boot with the kargs word `net=dhcp` (`build/go-dev-dhcp.img` has it). The kernel asks before the stack starts, logs `[net] INFO: eth0 dhcp 10.0.2.15/24 via 10.0.2.2, lease 24h0m0s from 10.0.2.2`, renews the lease halfway through over UDP, and `net` on the console shows it. Without an answer in about 8 s it uses the fixed addresses. QEMU's user network always gives 10.0.2.15, so on it DHCP changes nothing but the source of the numbers; `/etc/resolv.conf` is rewritten from the lease only when `/etc` is writable (an ext2 root).
