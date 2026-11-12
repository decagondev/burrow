# Roadmap

## Milestones

| M | Name | Demo you can show someone | Wave |
|---|---|---|---|
| M0 | Foundations | `make test` green on an empty-ish repo | W0 |
| M1 | It boots | text from our own boot sector on serial + screen | W1 |
| M2 | Long mode | stage2 reaches 64-bit and calls C | W2 |
| M3 | C loader | `BURROW:LOADER:OK` with every input verified | W3 |
| M4 | Higher half | nucleus runs at `0xFFFFFFFF80100000` with the physmap | W4 |
| M5 | Nucleus alive | exceptions dump registers; two kernel threads ping-pong via futex | W5 |
| M6 | **Go says hello** | stock Go binary prints from ring 0; goroutines + GC stress pass | W6 |
| M7 | Go drives hardware | typing on the keyboard echoes through a Go driver | W7 |
| M8 | Files + pixels | `cat /etc/motd` from initrd; Go draws on the framebuffer | W8 |
| M9 | First process | static musl `hello` runs in ring 3 | W9 |
| M10 | A Unix | busybox shell, pipes, fork/exec | W10 |
| M11 | **DOOM** | Freedoom E1M1 playable in QEMU (done: `make doom-smoke`) | W11 |
| M12 | Ports | Lua REPL, C++ hello with exceptions, Go user program | W12 |
| M13 | A workbench | edit, save and build code inside Burrow; files survive a reboot | W13 |
| M14 | Desktop groundwork | a mouse-driven paint program whose colour arrives over a Unix socket (done: `make desk-smoke`) | W14 |
| M15 | Qt | a Qt text editor on `/dev/fb0` edits and saves a file in `/home` (done: `make qt-smoke`) | W15 |
| M16 | A desktop | `startx`: dwm tiles st terminals, dmenu starts Doom in a window (done: `make x11-smoke`, `make run-desktop`) | W16 |
| M17 | On the network | busybox `wget` from the host, a Go program fetching an HTTPS page, `httpd` inside Burrow reached from the host (done: `make net-smoke`, `make run-net`) | W17 |
| M18 | Bigger programs | Doom in X through MIT-SHM at three quarters of its fbdev rate, a 64 MiB process forks in milliseconds, a dynamic program and `dlopen` (done: `make mem-bench`, `make x11-bench`, `make ports-smoke`) | W18 |
| M19 | A multi-user Unix | boot an ext2 root disk to `login:`, a user denied `/etc/passwd`, `su` with a password, `adduser`, a symbolic link; all still there after a reboot, `e2fsck` clean (done: `make unix-smoke`, `make run-unix`) | W19 |
| M20 | Many CPUs, hardened | four CPUs online, `nproc` 4, a four-thread program at least 2.5 times faster, `GOMAXPROCS=4`, SMEP and SMAP on, ACPI poweroff, `make test` green on four CPUs (done: `make smp-bench`, 3.8x for C and 4.0x for Go; `make test` with `SMP=4` and `SMP=1`) | W20 |
| M21 | A firmware framebuffer | stage2 sets a VBE mode; the console, `/dev/fb0` programs, Doom and X draw at 1280x1024 with a padded pitch from boot_info; the framebuffer is write-combining; DISPI still serves images that ask for no mode (done: `make fb-smoke`, `make vbe-smoke`) | W21 |
| M22 | Sound and an absolute pointer | Doom's sound effects through AC'97, recorded and checked; `tone`'s 440 Hz measured; the pointer in X, Qt and paint where the host's is, with no grab (done: `make sound-smoke`, `make tablet-smoke`, `desktop-smoke` with both) | W22 |
| M23 | /proc and virtio devices | busybox `ps`, `top`, `free`, `tty`, `df` and `mount` work and `top` sees CPU time; the root and home disks on virtio-blk, the network on virtio-net with DHCP; `logger` reaches `syslogd` (done: `make proc-smoke`, `make virtio-smoke`) | W23 |
| M24 | ssh, TLS and a browser | the host logs in to Burrow over ssh (password, key, interactive, `scp`), `dbclient` logs in over loopback; `wget` and `curl` fetch over TLS and refuse a bad certificate; NetSurf renders pages in X, follows a link, loads `https://` (done: `make tls-smoke`, `make ssh-smoke`, `make browser-smoke`) | W24 |
| M25 | Cheaper round trips | `make lat-bench` prints a round trip's steps; `getpid` about half (103 to 52 us), clocks through a vDSO (85 ns); BUG-0033 closed; burrow runs the desktop on the ext2 root (`make unix-desktop-smoke`). Not reached: the ping-pongs and `scp` (1.25 MB/s) twice as fast (R-09) | W25 |
| M26 | Booting from UEFI | OVMF boots a GPT disk's `BOOTX64.EFI`; the nucleus, the console, Doom and the desktop run with GOP's framebuffer and the firmware's ACPI tables, on four CPUs and on one; the BIOS images boot as before; a USB-stick image and a real-hardware guide (planned: `make uefi-smoke`, `make uefi-usb`) | W26 |
| M27 | A real PC's devices | q35 with OVMF and no legacy devices boots one USB stick: interrupts through the I/O APIC and MSI, volumes on USB, SATA and NVMe, a USB keyboard and mouse driving the console, Doom and the desktop; the stick and an initrd-only image ready for a real PC, with a report of what it found (planned: `make q35-smoke`, `make usb-smoke`, `make usb-desktop-smoke`) | W27 |

```mermaid
flowchart TB
    subgraph boot["Boot chain"]
        direction LR
        W0["W0<br/>foundations"] --> W1["W1<br/>stage1"] --> W2["W2<br/>stage2"] --> W3["W3<br/>C loader"]
    end
    subgraph core["Nucleus and Go kernel"]
        direction LR
        W4["W4<br/>higher half"] --> W5["W5<br/>nucleus core"] --> W6["W6<br/>Go runtime"] --> W7["W7<br/>Go drivers"] --> W8["W8<br/>VFS + fb"]
    end
    subgraph unix["User space"]
        direction LR
        W9["W9<br/>processes"] --> W10["W10<br/>POSIX + shell"] --> W11["W11<br/>DOOM"] --> W12["W12<br/>ports"] --> W13["W13<br/>workbench"]
    end
    subgraph desk["Desktop"]
        direction LR
        W14["W14<br/>desktop groundwork"] --> W15["W15<br/>Qt"] --> W16["W16<br/>X11 desktop"] --> W17["W17<br/>networking"] --> W18["W18<br/>foundations"] --> W19["W19<br/>a multi-user Unix"] --> W20["W20<br/>many CPUs, hardened"] --> W21["W21<br/>firmware framebuffer"] --> W22["W22<br/>sound, a tablet"] --> W23["W23<br/>/proc, virtio"] --> W24["W24<br/>ssh, a browser"] --> W25["W25<br/>speed"] --> W26["W26<br/>UEFI"] --> W27["W27<br/>a real PC's devices"] --> X["later: gcc, Python,<br/>GTK"]
    end
    boot --> core --> unix --> desk
    classDef done fill:#2e7d32,color:#fff;
    classDef next fill:#f9a825,color:#000;
    classDef later fill:#e6eaee,color:#333,stroke-dasharray: 4 3;
    class W0,W1,W2,W3,W4,W5,W6,W7,W8,W9,W10,W11,W12,W13,W14,W15,W16,W17,W18,W19,W20,W21,W22,W23,W24,W25 done;
    class W26 next;
    class X later;
```

Green = done, yellow = next, grey = planned.

## Critical path and risk

The critical path was **W5 -> W6**: Go could not run until the nucleus had threads, futexes and a reserve/commit VM, and W6 held the riskiest spike (ADR-0003); both are done. Everything since has been "normal" OS work with many reference implementations. W14 laid the desktop groundwork (more RAM, a mouse, the start-up calls, a clock, file mappings, Unix sockets with descriptor passing, the console's graphics modes); W15 put Qt widget programs on `/dev/fb0`; W16 an X11 desktop with dwm. W17 put it on the network: an e1000 driver, gVisor's TCP/IP stack in the kernel, `AF_INET` sockets, names, servers inside Burrow and HTTPS from a Go program. W18 laid the foundations bigger programs want: copy-on-write fork, lazy `execve`, shared memory with MIT-SHM, dynamic linking. W19 made it a multi-user Unix: ext2 read and written, users and groups, permissions, links, and an ext2 root disk that boots to `login:`. W20 hardened it and put it on every CPU: SMEP, SMAP, XSAVE and ACPI, application processors behind per-CPU GS, one run queue, TLB shootdown, and the Go kernel with a P per CPU. W21 (E02.F04, E07.F04) took the framebuffer from the firmware: VBE in stage2, a boot_info descriptor, any geometry and pixel format, write-combining, the bring-up step real hardware needs that QEMU's DISPI device let us skip. W22 (E18.F05-F07) added sound (AC'97, OSS's `/dev/dsp`, Doom's effects) and an absolute pointer (virtio-input over a virtio transport that block and network devices can use later), on shared interrupt lines. W23 (E20) gave Burrow `/proc` and CPU time, so the tools that look inside a Unix work, and virtio-blk, virtio-net and DHCP on W22's transport, now the run targets' default. W24 (E21) put ssh in and out (dropbear), TLS for C programs (mbedTLS, curl) and NetSurf, a graphical browser, on the X desktop, and measured what a system call costs. W25 (E22) measured the round trip step by step, put the clocks in a vDSO, halved a call that cannot block and let burrow run the desktop; a blocking call's goroutine hand-off is what is left (R-09). W26 (E23) is next: a UEFI loader beside the BIOS chain, the machine described by the firmware, the screen from GOP, and a USB-stick image; then the drivers a real PC needs (AHCI, xHCI, NVMe). See [RISKS.md](../living/RISKS.md).

## Rough effort (part-time, one person)

| Waves | Estimate | Note |
|---|---|---|
| W0-W3 | done | re-typing them for learning: 1-2 weekends |
| W4-W5 | 3-5 weekends | lots of new concepts; budget debugging time |
| W6 | 2-6 weekends | widest uncertainty |
| W7-W8 | 3-4 weekends | |
| W9-W10 | 5-8 weekends | processes are the second big hump |
| W11 | 1-3 weekends | mostly fbdev/evdev/mmap glue if W10 is solid |
| W12 | open-ended | |
