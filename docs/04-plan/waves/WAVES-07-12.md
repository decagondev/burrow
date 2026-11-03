# Waves 7-12 (story level)

Sliced when the previous wave closes. Story IDs refer to [BACKLOG.md](../BACKLOG.md).

## W7 Go Drives Hardware (M7) — sliced: [WAVE-07](WAVE-07-go-drivers.md)
Stories: E06.F01 HAL + nucleus wrappers · E06.F02 driver registry · E06.F03 IRQ goroutines + PS/2 · E06.F04 console line editor · E06.F05 panic path · E06.F06 `hostsim` · E06.F07 PCI.
Demo: typing echoes via the Go keyboard driver; `lspci`-style listing on boot.
Likely slices: nucleus `nu_irq_wait/ack` + ktest; Go `IRQ` (from snippets); PS/2 init + scancode set 1 decoder (pure, host-tested); console; `hostsim` build with fake ports and a channel-driven IRQ.

## W8 Files and Pixels (M8) — sliced: [WAVE-08](WAVE-08-files-pixels.md)
Stories: E07.F01 initrd tarfs · E07.F02 VFS · E07.F03 devfs · E07.F04 framebuffer (BGA) · E07.F05 block + ATA in Go · E07.F06 FAT32 read.
Demo: `cat /etc/motd`; Go draws a gopher-coloured gradient and a text console on the framebuffer.
Test idea: VFS conformance suite run against tarfs, ramfs and devfs (Liskov).

## W9 First Process (M9) — sliced: [WAVE-09](WAVE-09-first-process.md)
Stories: E08.F01 address spaces · E08.F02 user ELF loader · E08.F03 ring 3 · E08.F04 syscall ring · E08.F05 first syscalls · E08.F06 POSIX_COVERAGE.md.
Demo: `/bin/hello` (static musl) prints and exits; user page fault kills only that process.
First user program is 20 lines of asm doing `write` + `exit`, before musl.

## W10 A Unix (M10) — sliced: [WAVE-10](WAVE-10-a-unix.md)
Stories: E09.F01-F05. Demo: busybox `sh`; `ls | wc -l`; `^C` interrupts a loop.

## W11 DOOM (M11) — sliced: [WAVE-11](WAVE-11-doom.md)
Stories: E10.F01-F05 (F06 stretch).
Order: 1) build `doom` against musl and run it on a Linux VM framebuffer console (proves the backend). 2) fbdev ioctls + mmap on Burrow, test with a tiny `fbtest` program. 3) evdev from PS/2, test with `evtest`-like program. 4) Freedoom WAD in initrd. 5) Doom. 6) FPS counter and tuning.

## W12 Ports (M12)
Stories: E11. Order by difficulty: Lua → C++ hello → C++ exceptions/threads → Go user program → a compiler on Burrow. Write `docs/06-guides/PORTING.md` as you go.
