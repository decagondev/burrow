# Architecture Decision Records

An ADR records one significant decision: context, options, choice, consequences. ADRs are **never edited after acceptance** except to change Status to `Superseded by ADR-xxxx`. Copy `ADR-0000-template.md` to add one.

| ADR | Title | Status |
|---|---|---|
| [0001](ADR-0001-language-layering.md) | Assembly, then C, then Go | Accepted |
| [0002](ADR-0002-own-bios-bootloader.md) | Own BIOS bootloader first, UEFI later | Accepted |
| [0003](ADR-0003-go-runtime-hosting.md) | Host the stock Go runtime on a Linux-syscall shim | Accepted (spike W6-S01 confirms) |
| [0004](ADR-0004-syscall-gate.md) | One SYSCALL gate, caller classified by RIP range | Proposed |
| [0005](ADR-0005-irq-as-wakeups.md) | Interrupts become goroutine wake-ups | Accepted |
| [0006](ADR-0006-linux-abi-userland.md) | Linux x86-64 syscall ABI + musl for user space | Accepted |
| [0007](ADR-0007-test-strategy.md) | Serial markers + QEMU debug-exit as the test interface | Accepted |
| [0008](ADR-0008-disk-manifest.md) | Raw disk + manifest, filesystem later | Accepted |
| [0009](ADR-0009-gvisor-tcpip.md) | gVisor's TCP/IP stack in the kernel, vendored | Accepted |
| [0010](ADR-0010-ext2.md) | ext2 as Burrow's Unix file system | Accepted |
