# Debugging Guide

## 1. First moves for any failure

1. Read the serial log: `build/smoke-*.log`, or `make run`.
2. The last marker tells you which slice to suspect.
3. One-letter codes are in [BOOT_PROTOCOL.md §5](../02-architecture/BOOT_PROTOCOL.md).
4. If QEMU resets or hangs, use the tools below, then open a BUG entry *before* fixing.

## 2. QEMU flags

| Flag | Use |
|---|---|
| `-d int,cpu_reset -D build/qemu.log` | log every interrupt/exception and the CPU state at a triple fault |
| `-no-reboot -no-shutdown` | stop instead of rebooting on triple fault, so you can inspect |
| `-monitor stdio` (instead of `-serial mon:stdio`) | `info registers`, `info mem`, `info tlb`, `x /16gx 0x1000` |
| `-s -S` | gdb server on :1234, start paused (`make debug`) |

Triple fault recipe: run with `-d int,cpu_reset -no-reboot`, search the log for the **first** `v=0e` (page fault) or `v=0d` (GP) before `check_exception old: 0x8` → that RIP and CR2 are the culprit.

## 3. GDB across CPU modes

```text
make debug                  # terminal 1
gdb -x scripts/gdbinit      # terminal 2: stops at 0x7c00 in i8086 mode
(gdb) x/8i $pc              # 16-bit disassembly (cs*16 + ip on real mode!)
(gdb) set architecture i386:x86-64
(gdb) add-symbol-file build/loader.elf
(gdb) break loader_main
(gdb) continue
(gdb) x/16gx 0x1000         # boot_info
(gdb) x/7gx 0x2000          # first E820 entries (24 bytes each)
```

In real mode gdb ignores segments: an address `ds:si` is `ds*16+si`. Use `p/x $ds*16+$si`.

## 4. Reading a GDT descriptor

`0x00AF9A000000FFFF` → limit 0xFFFF, base 0, access `0x9A` (present, ring 0, code, readable), flags `0xA` (G=1, L=1 long mode, D=0). Data `0xCF92...`: access `0x92` (present, data, writable), flags `0xC` (G=1, D=1).

## 5. Common traps

| Symptom | Usual cause |
|---|---|
| Reboot loop right after `[stage2] entering protected mode` | GDT pointer wrong / far jump selector wrong |
| Reset right after enabling paging | page tables not identity-mapping the code you are executing |
| Loader prints garbage | stack not aligned, or SSE used without enabling (`-mgeneral-regs-only` missing) |
| Random corruption in interrupt-heavy code | red zone (`-mno-red-zone` missing) |
| Works in QEMU, fails on hardware | uninitialised memory assumed zero, or BIOS-reserved memory used |
| Go runtime crashes early (W6) | missing syscall (check `[shim] UNIMPLEMENTED`), FS base not restored on switch, SSE off |
