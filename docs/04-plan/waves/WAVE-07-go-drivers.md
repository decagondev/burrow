# W7 Go Drives Hardware  (M7)  — status: in progress

Goal: drivers written in plain Go, woken by interrupts the nucleus turns into thread wake-ups ([ADR-0005](../../03-decisions/ADR-0005-irq-as-wakeups.md)). Demo: typing at the keyboard echoes through the Go PS/2 driver into a console line editor; PCI devices are listed at boot. Stories: E06.F01-F07 ([BACKLOG.md](../BACKLOG.md)).

Interrupt routing in this wave: the ISA lines (0-15) come through the 8259 pair in virtual-wire mode (LAPIC LINT0 = ExtINT), already remapped to `0x20-0x2F`. An I/O APIC (ACPI MADT) is not needed for QEMU's `pc` machine and waits for SMP or PCI MSI.

### [x] W7-S01 `nu_irq_wait` / `nu_irq_ack` (E06.F01)
`nucleus/irq.c`: one waiter per ISA line. The ISR masks the line in the 8259, sends EOI to the 8259 (not the LAPIC: ExtINT), sets `pending` and wakes the waiter. `irq_wait(n)` unmasks the line on first use and blocks until `pending`; `irq_ack(n)` unmasks it again. Spurious IRQ 7/15 (ISR bit clear) are dropped (PIT-28). An interrupt that fires before anyone waits is kept as `pending` (PIT-29). Shim rows `0x1000`/`0x1001`. ktests (suite `irq`): PIT channel 0 one-shot through IRQ 0; pending-before-wait; the line stays masked until ack; the call through the `syscall` gate.

### [x] W7-S02 Go `IRQ` (E06.F01, E06.F03)
Copy `snippets/kernel/irq_goroutine.go` into `kernel/arch/amd64/irq.go` (`syscall.Syscall`, so the runtime hands the P to another thread while one waits). `haltest.FakeIRQ`: channel-driven, for host tests. On target: `[go-selftest] irq ok` from a check that programs PIT channel 0 from Go and waits on line 0.

### [x] W7-S03 Driver registry + PCI (E06.F02, E06.F07)
`kernel/driver`: `Driver{Name, Probe, Start}`, `Register` from `init()`, `StartAll(env)` in name order; `Env` carries the HAL (ports, IRQ lines), a logger and the console input channel. `hal.PortIO32` (`In32/Out32`) for configuration mechanism #1 (`0xCF8/0xCFC`). `drivers/pci`: pure enumeration over a `ConfigSpace` interface (host-tested with a fake), printing `[pci] bb:dd.f vvvv:dddd class`.

### [x] W7-S04 PS/2 keyboard (E06.F03)
`drivers/ps2`: 8042 bring-up (self-test, config byte with IRQ 1 and translation on, enable scanning) and an IRQ 1 goroutine. Scancode set 1 decoder is pure and host-tested: shift, caps lock, ctrl (control codes), `E0` prefixes, releases; arrows become ANSI escapes.

### [x] W7-S05 Console (E06.F04)
`kernel/console`: a pure line editor (printable echo, backspace, `^U`, enter) fed by the keyboard and by COM1 receive interrupts (IRQ 4), with a few commands: `help`, `echo`, `lspci`, `uptime`, `mem`, `poweroff`. `make go-smoke` types into the guest through QEMU's monitor (`sendkey`) and checks the echoed line.

### [x] W7-S06 Panic path (E06.F05, PIT-07)
A Go panic prints its message and every goroutine (`GOTRACEBACK=all`) on serial, then `exit_group(2)` fails the run instantly (debug-exit `0x11`). A fault whose RIP is in Go text says so and names the tool to symbolize it. A `kargs` boot module becomes extra `argv` words, so a test image can ask the kernel to panic: regression case REG-0006.

### [ ] W7-S07 `hostsim` (E06.F06)
The Go kernel's driver stack (registry, PS/2, console) running as a normal host process and in `go test`, with fake ports and a channel-driven IRQ: inject scancodes, read the console's output.

Exit: **M7 demo — Go drives hardware.** `make clean test` green; DEVLOG wave entry.
