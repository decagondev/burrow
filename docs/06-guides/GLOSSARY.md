# Glossary

| Term | Meaning |
|---|---|
| A20 | Address line 20; must be enabled to reach memory above 1 MiB without wrap-around |
| ABI | Binary-level contract: struct layouts, register usage, syscall numbers |
| auxv | Auxiliary vector: (type, value) pairs a kernel gives a new process (page size, random bytes...) |
| BGA | Bochs Graphics Adapter: simple framebuffer mode-setting in QEMU via ports 0x1CE/0x1CF |
| boot_info | Our struct passed from stage2 upward with everything learned during boot |
| DAP | Disk Address Packet for INT 13h extended reads |
| E820 | BIOS memory-map call (INT 15h, EAX=E820h) |
| evdev | Linux input event interface (`struct input_event`) |
| fbdev | Linux framebuffer device interface (`/dev/fb0`, ioctls, mmap) |
| futex | "fast userspace mutex": wait if `*addr == val`, wake N waiters |
| GDT / IDT / TSS | Segment descriptor table / interrupt table / task-state segment (holds RSP0 and IST stacks) |
| IST | Interrupt Stack Table: lets an interrupt switch to a known-good stack |
| ktest | In-kernel test runner (W5) |
| LSTAR / STAR / FMASK | MSRs configuring the SYSCALL instruction |
| manifest | Our disk table of contents at LBA 1 |
| marker | A stable serial line tests look for |
| M (Go) | A Go runtime OS thread; each is a nucleus thread here |
| nucleus | Resident C/asm layer: mechanisms + Linux syscall shim for the Go runtime |
| physmap | Linear mapping of all physical RAM into the upper half |
| PT_LOAD | ELF program header type for segments to load |
| red zone | 128 bytes below RSP that SysV code may use; unsafe in kernels |
| shim | Table of Linux syscall implementations for the Go runtime |
| slice | Smallest committable unit of planned work, with its own test |
| wave | A time-box of slices ending in a milestone demo |
