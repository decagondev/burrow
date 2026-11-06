# Burrow Architecture

> Status: **living document**. Sections marked *(provisional)* are validated by a spike slice before they are relied on. Last reviewed: Wave 3 complete.

This is the map of the whole system. Read it once end-to-end, then come back to the section that matches the wave you are working on.

## 1. The one-paragraph version

Burrow boots through **four layers, each written in the language that suits it best**. Assembly does what only assembly can (the first 512 bytes, CPU mode switches). C does the careful, byte-exact work of loading images and owning the hardware *mechanisms* (page tables, interrupts, context switches). Go runs on top as the actual kernel, owning *policy* (processes, files, drivers, POSIX). The trick that makes Go possible is that the C layer (the **nucleus**) pretends to be a tiny Linux: the stock Go compiler builds the kernel as an ordinary `linux/amd64` program, and every system call the Go runtime makes is answered by the nucleus. Later, user programs built against musl libc speak the very same Linux system-call ABI, so Doom, Lua and friends port with minimal changes.

## 2. Layers

```mermaid
flowchart TB
    subgraph U["Ring 3 - user space (Wave 9+)"]
        direction LR
        doom["DOOM<br/>(doomgeneric + musl)"]
        sh["shell, busybox"]
        lua["Lua, C, C++, Go programs"]
    end
    subgraph K["Ring 0 - Go kernel (policy)"]
        direction LR
        posix["POSIX syscall layer"]
        proc["process manager"]
        vfs["VFS + filesystems"]
        drv["drivers<br/>(uart, ps2, fb, ata)"]
        hal["HAL interfaces"]
    end
    subgraph RT["Go runtime (stock, unmodified)"]
        gort["scheduler, GC, goroutines"]
    end
    subgraph N["Ring 0 - nucleus, C + asm (mechanism)"]
        direction LR
        shim["Linux syscall shim<br/>(for the Go runtime)"]
        sched["threads + scheduler"]
        vm["physical + virtual memory"]
        irq["IDT, exceptions, IRQ to wake-up"]
    end
    subgraph B["Boot chain (runs once)"]
        direction LR
        s1["stage1.asm<br/>512 B MBR"] --> s2["stage2.asm<br/>real to long mode"] --> ldr["loader (C)<br/>loads ELF images"]
    end
    HW[("x86-64 hardware / QEMU")]

    U -- "SYSCALL (Linux ABI)" --> N
    N -- "syscall ring: forwarded to Go" --> posix
    K --> RT
    RT -- "SYSCALL (Linux ABI subset)" --> shim
    drv --> hal
    hal -- "port I/O, MMIO, nu_* calls" --> N
    N --> HW
    B -- "boot_info + images" --> N
```

| Layer | Language | Owns | Never does |
|---|---|---|---|
| stage1 | NASM, 16-bit | load stage2 | anything else (512-byte budget) |
| stage2 | NASM, 16/32/64-bit | A20, memory map, mode switches, verify + start loader | parse ELF, drive hardware beyond BIOS |
| loader | freestanding C | read disk, verify images, load ELF, build first real page tables, fill `boot_info` | stay resident after hand-off |
| nucleus | C + asm | IDT/GDT/TSS, PMM/VMM mechanism, threads, futexes, timers, syscall gate, Linux shim for the Go runtime | file systems, process policy, POSIX semantics |
| Go kernel | Go (+ tiny Go asm) | drivers, VFS, processes, POSIX syscalls, console | interrupt handlers (it receives *wake-ups*, not interrupts) |
| user space | C/C++/Go/Lua + musl | applications | — |

**Why split mechanism (C) from policy (Go)?** Everything that must run with interrupts off, on a fixed stack, without allocating, lives in C. Go's runtime can allocate, grow stacks and pause for GC at almost any function call, which is fine for policy and lethal inside an interrupt handler. This split is the single most important design decision; see [ADR-0003](../03-decisions/ADR-0003-go-runtime-hosting.md).

## 3. Boot sequence

```mermaid
sequenceDiagram
    autonumber
    participant BIOS
    participant S1 as stage1 (0x7C00)
    participant S2 as stage2 (0x7E00)
    participant L as loader (0x10000)
    participant N as nucleus (higher half)
    participant G as Go kernel
    BIOS->>S1: load LBA 0, jump, DL = boot drive
    S1->>S1: segments, stack, COM1 init
    S1->>S2: INT 13h AH=42h reads LBA 2..33
    S2->>S2: A20, E820 map, read manifest (LBA 1)
    S2->>S2: load "loader" entry to 0x10000
    S2->>S2: CPUID long-mode check, fill boot_info
    S2->>S2: protected mode, CRC-check loader
    S2->>S2: identity paging, EFER.LME, long mode
    S2->>L: call 0x10000 with RDI = boot_info
    L->>L: verify boot_info, memory map, manifest, ATA
    L->>L: W4 - read nucleus ELF (CRC), copy segments, page tables, boot_info v2
    L->>N: W4 - load CR3, jump with RDI = physmap + 0x1000
    N->>N: W5 - GDT/TSS/IDT, PMM, VMM, LAPIC timer, threads, futexes, SSE
    N->>G: W6 - load Go ELF, Linux-style stack, enter _rt0_amd64_linux
    G->>N: runtime syscalls (mmap, clone, futex, ...)
    G->>G: W7 - main() - self-tests, driver registry, console
    G->>N: driver goroutine: nu_irq_wait(n) blocks its thread
    N->>G: ISA IRQ n - ISR masks, EOIs, wakes the thread
    G->>N: nu_irq_ack(n) after draining the device
    G->>G: W8 - VFS (initrd, ramfs, devfs, FAT32), framebuffer, ATA in Go
    G->>G: W9+ - processes, init
```

## 4. CPU mode state machine

```mermaid
stateDiagram-v2
    [*] --> Real16: power on / BIOS
    Real16 --> Real16: stage1, stage2 BIOS calls
    Real16 --> Protected32: lgdt + CR0.PE + far jmp
    Protected32 --> Long64: PAE + CR3 + EFER.LME + CR0.PG + far jmp
    Long64 --> Long64: loader, nucleus, Go kernel
    Long64 --> Ring3: iretq / sysretq (Wave 9)
    Ring3 --> Long64: SYSCALL, interrupt, exception
    Real16 --> Halted: any FAIL code
    Protected32 --> Halted: CRC mismatch
    Long64 --> Halted: PANIC
    Halted --> [*]
```

## 5. Component view (static structure)

```mermaid
flowchart LR
    subgraph repo["Repository"]
        boot["boot/<br/>stage1, stage2,<br/>boot_abi.inc"]
        inc["include/burrow/<br/>boot_info.h,<br/>manifest.h"]
        loader["loader/<br/>C modules"]
        nucleus["nucleus/<br/>(W4+)"]
        kernel["kernel/<br/>Go module"]
        tools["tools/mkimage<br/>Go host tool"]
        tests["tests/<br/>unit, smoke,<br/>regression"]
    end
    boot -. "same bytes as" .- inc
    loader --> inc
    nucleus --> inc
    tools -. "same bytes as" .- inc
    tools -- "builds" --> img[("build/disk.img")]
    boot --> img
    loader --> img
    tests -- "boots" --> img
```

The `include/burrow/` headers are **contracts** between components written in different languages. Each contract is mirrored in exactly one other place (`boot/boot_abi.inc` for asm, `tools/mkimage/manifest.go` for Go) and is protected by tests: `_Static_assert` offsets in C, `TestManifestLayoutMatchesC` in Go, and the smoke test end-to-end.

## 6. Interfaces (UML)

### 6.1 C "interfaces" (struct of function pointers)

```mermaid
classDiagram
    class char_sink {
        +putc(ctx, c)
        +ctx
    }
    class block_device {
        +name
        +read(dev, lba, count, buf) int
        +ctx
    }
    class kprintf {
        +kprintf_set_sink(char_sink)
        +kvprintf_to(char_sink, fmt, ap) int
    }
    class serial_16550 {
        +serial_init(base)
        +serial_sink(base) char_sink
    }
    class ata_pio {
        +ata_pio_open(channel, block_device) int
    }
    class test_buffer_sink
    kprintf ..> char_sink : depends on
    serial_16550 ..|> char_sink : provides
    test_buffer_sink ..|> char_sink : provides (host tests)
    ata_pio ..|> block_device : provides
    class image_loader_W4 {
        +load_entry(block_device, manifest_entry)
    }
    image_loader_W4 ..> block_device : depends on
```

### 6.2 Go kernel HAL

```mermaid
classDiagram
    class PortIO {
        <<interface>>
        +In8(port) uint8
        +Out8(port, v)
    }
    class Clock {
        <<interface>>
        +Nanotime() int64
    }
    class IRQLine {
        <<interface>>
        +Wait() error
        +Ack()
    }
    class Ports {
        arch/amd64, tag burrow
    }
    class FakePorts {
        haltest, host tests
    }
    class IRQ {
        arch/amd64, nu_irq_wait
    }
    class UART {
        +Init()
        +WriteByte(c) error
        +Write(p) int, error
    }
    class Logger {
        +Infof(f, args)
        +With(subsystem) Logger
    }
    Ports ..|> PortIO
    FakePorts ..|> PortIO
    IRQ ..|> IRQLine
    UART --> PortIO : uses
    Logger --> UART : io.Writer
```

## 7. Data flows

### 7.1 A Go runtime system call (Wave 6)

```mermaid
sequenceDiagram
    participant GR as Go runtime (ring 0)
    participant GATE as syscall_gate.asm
    participant SHIM as syscall_shim.c
    participant VM as nucleus vm
    GR->>GATE: SYSCALL (RAX=9 mmap, RCX=return RIP)
    GATE->>GATE: RCX inside Go text range, so caller = Go
    GATE->>GATE: switch to per-thread kernel stack
    GATE->>SHIM: nu_syscall_dispatch(frame)
    SHIM->>VM: vm_reserve(hint, len) - PROT_NONE, no RAM used
    VM-->>SHIM: address
    SHIM-->>GATE: RAX = address
    GATE-->>GR: IRETQ (never SYSRET to ring 0)
```

### 7.2 An interrupt becomes a goroutine wake-up (Wave 7)

```mermaid
sequenceDiagram
    participant HW as PS/2 keyboard
    participant ISR as nucleus ISR (IST stack)
    participant T as nucleus thread parked in nu_irq_wait(1)
    participant D as Go keyboard driver goroutine
    participant V as /dev/input/event0
    D->>T: syscall nu_irq_wait(1) - blocks this thread only
    HW->>ISR: IRQ1
    ISR->>ISR: mask IRQ1, send EOI, mark pending
    ISR->>T: make runnable
    T-->>D: Wait() returns
    D->>D: inb(0x60), decode scancode
    D->>V: queue struct input_event
    D->>T: nu_irq_ack(1) - unmask
```

### 7.3 A user system call (Wave 9, full frames since Wave 10)

```mermaid
sequenceDiagram
    participant P as user process (ring 3)
    participant GATE as syscall gate
    participant RING as request queue (nucleus/uthread.c)
    participant GD as Go dispatcher goroutine
    participant H as handler goroutine
    P->>GATE: SYSCALL read(fd, buf, n)
    GATE->>GATE: CR3 is not the kernel's, so caller = user, kernel stack
    GATE->>RING: queue request (tid, nr, args), block the user thread
    RING-->>GD: nu_syscall_wait returns (copies the requests out)
    GD->>H: go handle(req)
    H->>H: VFS read, copyout through the physmap
    H->>GATE: nu_syscall_reply(tid, result)
    H->>H: signal pending? write an rt_sigframe, nu_uthread_regs(set)
    GATE-->>P: IRETQ from the saved trap_frame (a non-canonical RIP becomes a fault first)
```

### 7.4 One DOOM frame (Wave 11)

```mermaid
flowchart LR
    tick["doomgeneric_Tick()"] --> render["render into DG_ScreenBuffer<br/>640x400 XRGB"]
    render --> draw["DG_DrawFrame():<br/>scale + copy"]
    draw --> fbmem[("mmap of /dev/fb0<br/>= linear framebuffer")]
    fbmem --> screen["QEMU display"]
    kbd["PS/2 IRQ1"] --> evdev["Go driver to<br/>/dev/input/event0"]
    evdev --> getkey["DG_GetKey():<br/>read() O_NONBLOCK"]
    getkey --> tick
    clock["LAPIC/PIT time"] --> ticks["clock_gettime:<br/>DG_GetTicksMs"]
    ticks --> tick
```

### 7.5 Many CPUs (Wave 20)

Every CPU the ACPI MADT lists comes online at boot (ADR-0011). The nucleus
copies a trampoline (`nucleus/ap.asm`) to physical 0x8000 and starts each
application processor with INIT and two STARTUP IPIs; the AP climbs from
real mode to long mode, loads its own GDT, TSS and IST stacks, binds
`GS_BASE` to its `struct percpu`, turns on its LAPIC timer and becomes its
own idle thread. From then on, all CPUs schedule from one run queue:

- **Per-CPU state through GS.** `this_cpu()` is one `gs:` load; `trap.asm`
  and `syscall.asm` read the current kernel stack at `gs:0`/`gs:8`. Ring 3
  runs with the user's GS base: every entry from ring 3 and every return to
  it swaps (`swapgs`), and SYSCALL has a user and a kernel entry, which the
  scheduler switches between in LSTAR.
- **One run queue.** `g_sched_lock` guards every thread's state and is held
  across the context switch; the thread switched to releases it. Waking a
  thread sends the wake IPI (0xF0) to an idle CPU that may run it. Lock
  order: an object's lock (futex bucket, IRQ lines, the uthread FIFO), then
  the scheduler's.
- **TLB shootdown.** Each CPU publishes the page-table root it runs on; a
  change to a present entry is flushed on every CPU on that root (all of
  them for the shared upper half) through IPI 0xF1 before the call returns
  and before a freed frame is reused. CPUs spinning on a lock with
  interrupts off answer shootdowns while they spin.
- **Go on every CPU.** The shim's `sched_getaffinity` reports the CPUs
  online, so the Go kernel runs with that many Ps; `kernel/proc` reports
  the same to user programs (`nproc`, `sysconf`, a Go program's
  GOMAXPROCS). Interrupts from devices still arrive on CPU 0 through the
  8259; a driver's goroutine may run anywhere.

```mermaid
flowchart LR
    bsp["CPU 0 (BSP)<br/>timer, 8259 IRQs"] --> rq[("one run queue<br/>g_sched_lock")]
    ap1["CPU 1..n (APs)<br/>own timer, own GDT/TSS"] --> rq
    rq --> th["kernel threads, Go's Ms,<br/>user threads"]
    vm["vm_unmap / vm_protect"] -- "IPI 0xF1, wait" --> ap1
    wake["thread_wake"] -- "IPI 0xF0 to an idle CPU" --> ap1
```

### 7.6 Framebuffer and firmware (Wave 21)

The screen's mode comes from the firmware, as on real hardware, not from
QEMU's Bochs registers. An image asks for a mode with a small `video`
payload (`mkimage -video WxHxB[,pitch]`); stage2's last real-mode step
(`boot/vbe16.inc`) walks the VBE mode list, sets a linear direct-colour
mode, asks for a padded scanline with 4F06 when told to, and records base,
size, pitch, depth and colour fields in boot_info (version 3). The nucleus
checks that descriptor (`nucleus/fbdesc.c`) and clears it if it is wrong;
the Go kernel's display driver maps it write-combining (the PAT's entry 1
is WC on every CPU) and hands `gfx` the geometry and pixel format, and only
an image that asked for nothing (or a mode the BIOS lacks) gets a DISPI mode
instead. `/dev/fb0` reports the mode as it is, `line_length` included, and
maps it into programs write-combining too, so a page never has two memory
types.

```mermaid
flowchart LR
    req["video payload<br/>WxHxB, pitch"] --> s2["stage2: VBE<br/>4F00 4F01 4F02 4F06"]
    s2 --> bi[("boot_info v3<br/>fb_* fields")]
    bi --> chk["nucleus: fbdesc check"]
    chk -- "ok" --> drv["display driver:<br/>map WC, gfx.Format"]
    chk -- "none or refused" --> dispi["DISPI 1024x768x32"]
    dispi --> drv
    drv --> con["console"] & fb0["/dev/fb0: X, Qt, Doom"]
```

Programs must take the geometry from `FBIOGET_VSCREENINFO` and step rows by
`line_length`; `make fb-smoke` boots 1280x1024 with a 5248-byte pitch to
keep that true.

### 7.7 Sound and input (Wave 22)

Two PCI devices arrived together, and QEMU put both on IRQ 11 beside the
e1000, so the wave started with **shared lines**: the nucleus still lets one
thread wait on a line, and in Go that thread is the line's goroutine
(`driver.Lines`), which calls every handler registered there; each reads its
own device's status and says whether the interrupt was its own (an
unclaimed one is counted and logged once).

**Sound** is an AC'97 (`drivers/ac97`): the mixer and the bus master are two
I/O BARs, and playback is a ring of 32 buffer descriptors pointing at DMA
pages. The driver fills buffers ahead of the card and moves the last valid
index forward after the data is written; an interrupt per completed buffer
frees room. `kernel/sound` puts OSS's playback subset over it as
`/dev/dsp`: rates through the codec's variable rate, 16-bit stereo native and
8-bit or mono converted, `write` blocking while the ring is full, one
opener. Doom mixes its effects in a thread and writes them there.

**The pointer** is QEMU's virtio tablet. `kernel/virtio` is the virtio 1.0
PCI transport, pure Go: the capabilities in configuration space, the status
handshake with `VIRTIO_F_VERSION_1` required, split virtqueues in DMA pages
whose indices are read and written as whole aligned words, notification
and the ISR. `drivers/virtioinput` keeps the event queue full of 8-byte
buffers; the device's events are already Linux input events, and
`kernel/evdev` makes them `/dev/input/event2` (`EV_ABS` with `EVIOCGABS`
ranges). Readers of the relative `/dev/input/mice` (TinyX) get the same
packets as moves: the position scaled to the current mode minus the last
one, a session starting at the screen's centre, and moves to an edge
overshot by a screen so a reader that lost packets clamps back in step.

```mermaid
flowchart LR
    irq11["IRQ 11<br/>(8259 line)"] --> lines["driver.Lines<br/>one goroutine per line"]
    lines --> e1000["e1000"] & ac97["ac97: ring of 32<br/>DMA buffers"] & vin["virtioinput:<br/>event queue"]
    ac97 --> dsp["/dev/dsp<br/>(OSS)"] --> doom["Doom, tone"]
    vin --> ev2["/dev/input/event2<br/>EV_ABS"] --> paintqt["paint, Qt (abs)"]
    ev2 --> mice["/dev/input/mice<br/>scaled moves"] --> x["TinyX (xaccel: 1/1)"]
```

The virtio transport is meant for more than input: virtio-blk and
virtio-net would use the same capabilities, negotiation and queues.

## 8. Design principles applied (SOLID and friends)

| Principle | Where you will see it |
|---|---|
| **S**ingle responsibility | One file = one job: `crc32.c`, `elf64.c`, `manifest.c`. Stage1 *only* loads stage2. The shim has one function per syscall. |
| **O**pen/closed | New syscall = new table row. New disk controller = new `block_device` provider. New boot stage = new smoke markers appended, old ones never edited. |
| **L**iskov substitution | Every fake (`FakePorts`, buffer `char_sink`) must be usable wherever the real one is. Conformance tests per interface arrive with the second implementation (W8: ramdisk vs ATA). |
| **I**nterface segregation | `PortIO` has 2 methods, `IRQLine` 2, `char_sink` 1. Drivers ask for the smallest interface they need. |
| **D**ependency inversion | `kprintf` depends on `char_sink`, not the UART. Go drivers depend on `hal`, implementations are injected in `main`. This is *why* most kernel code runs as plain host unit tests. |
| Pure core, thin shell | Parsing/validation (ELF, manifest, CRC, printf, memory map) is pure and host-tested; hardware access is pushed to thin edges (`io.h`, `arch/amd64`). |
| Verify before execute | stage2 CRC-checks the loader before jumping; the loader will CRC-check the nucleus and kernel ([BUG-0003](../living/BUGS_AND_MITIGATIONS.md)). |
| Contracts are tested | Every cross-language struct has offset assertions on both sides. |

## 9. Where to go next

The physical and virtual memory layouts are in [MEMORY_MAP.md](MEMORY_MAP.md). Boot data structures, error codes and markers are in [BOOT_PROTOCOL.md](BOOT_PROTOCOL.md). The Go/nucleus contract is in [GO_RUNTIME_HOSTING.md](GO_RUNTIME_HOSTING.md), and the private nucleus calls in [NUCLEUS_ABI.md](NUCLEUS_ABI.md).
