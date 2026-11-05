# Boot Protocol

The exact contracts between boot components. If code and this document disagree, the code is wrong *or* this document is stale: fix one of them in the same commit and add a DEVLOG line.

## 1. Disk layout

| LBA | Sectors | Contents | Written by |
|---|---|---|---|
| 0 | 1 | stage1 (MBR), ends `0x55 0xAA` | `mkimage` from `build/stage1.bin` |
| 1 | 1 | manifest (`BRWMNFST`) | `mkimage` |
| 2 | 32 | stage2 slot (16 KiB, zero padded) | `mkimage` from `build/stage2.bin` |
| 34.. | n | payloads in `-payload` order: `loader`, later `nucleus`, `kernel`, `initrd` | `mkimage` |

The image is padded to at least 4 MiB. Inspect any image with `build/mkimage -inspect build/disk.img`.

## 2. Manifest (LBA 1)

Defined in `include/burrow/manifest.h`, mirrored in `boot/boot_abi.inc` and `tools/mkimage/manifest.go`.

| Offset | Size | Field | Notes |
|---|---|---|---|
| 0 | 8 | magic | ASCII `BRWMNFST`, no NUL |
| 8 | 2 | version | `1` |
| 10 | 2 | count | 1..11 |
| 12 | 4 | reserved | 0 |
| 16 + 40*i | 40 | entry i | see below |

Entry: `name[16]` (NUL padded, must end in NUL), `lba u32`, `sectors u32`, `size_bytes u64`, `crc32 u32` (IEEE, same as Go `hash/crc32.ChecksumIEEE`), `flags u32` (0).

Validation rules (`loader/manifest.c`, unit-tested): magic, version, count range, name terminated, `sectors*512 >= size_bytes`, every non-stage2 entry starts at LBA >= 34.

**The `video` payload (W21-S01).** `mkimage -video WxHxB[,pitch]` adds a 16-byte payload named `video`, `struct video_request` in `manifest.h` (mirrored by `VR_*` in `boot_abi.inc` and `tools/mkimage/video.go`): magic `VIDR` at 0, width u16 at 4, height u16 at 6, bpp u8 at 8, three reserved bytes, pitch_bytes u32 at 12 (0 = the mode's own scanline). stage2 reads it, as its last real-mode step, and sets a VBE mode: 4F00 for the mode list (`VBE2` in), 4F01 for each mode until one is linear (attributes bits 0, 4 and 7), direct colour (memory model 6) and of the requested width, height and depth, 4F02 with bit 14 (linear) to set it, then 4F06 BL=2 for a requested pitch, keeping what the card answers (it may round: 1700 bytes at 800x600x16 became 1696). It prints `[stage2] vbe: WxHxB pitch P at BASE (mode 0xM)`, or `[stage2] vbe: no WxHxB mode` and sets nothing; both go to COM1 only, because after 4F02 the BIOS teletype has no text mode to write to. Its scratch is at 0xC000-0xC5FF (MEMORY_MAP.md); the chosen ModeInfoBlock stays at 0xC200 for boot_info (S02). The loader reads `video` like any payload, so it also arrives as a 16-byte boot module. The Go images ask for 1024x768x32 (`VIDEO ?=` in the Makefile); `disk.img` and the ktest images ask for nothing.

## 3. `struct boot_info` (physical 0x1000)

Defined in `include/burrow/boot_info.h` with `_Static_assert` offsets.

| Offset | Field | Filled by | Meaning |
|---|---|---|---|
| 0x00 | magic u64 | stage2 | `"BOOTINFO"` little-endian |
| 0x08 | version u32 | stage2, loader | 1 from stage2; the loader sets 2 just before the nucleus hand-off |
| 0x0C | size u32 | stage2 | 0x80 |
| 0x10 | boot_drive u32 | stage2 | BIOS DL, e.g. 0x80 |
| 0x14 | e820_count u32 | stage2 | entries at e820_addr |
| 0x18 | e820_addr u64 | stage2 | 0x2000 |
| 0x20 | manifest_addr u64 | stage2 | 0x0600 |
| 0x28 | loader_base u64 | stage2 | 0x10000 |
| 0x30 | fb_addr u64 | W8 | linear framebuffer, 0 = none |
| 0x38..0x44 | fb_width/height/pitch/bpp | W8 | |
| 0x48 | physmap_base u64 | loader (v2) | virtual address of physical 0 (`0xFFFF800000000000`) |
| 0x50 | modules_addr u64 | loader (v2) | physical address of `struct boot_module[]` (`0x1100`) |
| 0x58 | module_count u32 | loader (v2) | valid entries, at most 16 |
| 0x5C | reserved0 u32 | — | 0 |
| 0x60 | reserved[4] | — | grow here, bump version |

`struct boot_module` (32 bytes): `name[16]` (NUL padded), `phys_start u64`, `size_bytes u64`, both 4 KiB multiples. The loader reports: one module per manifest payload that is not a boot stage (e.g. `ktest`; `kernel` = the Go kernel ELF; `kargs` = a text file whose whitespace-separated words the nucleus appends to the Go kernel's `argv` (W7); `initrd` = a ustar archive the Go kernel mounts at `/` (W8)), read into fresh frames and CRC-checked, with `size_bytes` = the exact payload size; then `nucleus` (the loaded segments at 1 MiB) and `loader-frames` (every frame `loader/palloc.c` handed out, which includes the payload modules' frames and the page tables now in CR3). The nucleus must treat both as in use. All addresses inside boot_info stay **physical**; the nucleus reaches them through `physmap_base + addr`.

**Versioning rule:** add fields only into `reserved`; bump `BOOTINFO_VERSION`; the consumer rejects versions it does not know. Stage2 writes version 1 (`BOOTINFO_VERSION_STAGE2`, v2 fields zero); the loader checks for 1, fills the v2 fields and writes 2 (`BOOTINFO_VERSION`). The nucleus accepts only 2.

## 4. Register hand-offs

| From | To | Mode | Registers | Stack |
|---|---|---|---|---|
| BIOS | stage1 | real 16 | `DL` = boot drive | BIOS's |
| stage1 | stage2 | real 16 | `DL` = boot drive | `0x7C00` |
| stage2 | loader | long 64 | `RDI` = &boot_info (SysV arg 1), interrupts OFF | `0x80000`, aligned per SysV at the call |
| loader | nucleus (W4) | long 64 | `RDI` = &boot_info v2 (physmap-relative address) | nucleus boot stack |
| nucleus | Go kernel (W6) | long 64, ring 0 | Linux process entry: `RSP` -> argc/argv/envp/auxv, `RDX` = 0 | built by `go_initial_stack.c` |

## 5. Failure codes

Every stage prints a one-letter code on COM1 and the screen (real mode only), then writes `0x11` to port `0xF4` (QEMU exits with status 35 in the harness; no-op on real hardware) and halts.

| Stage | Code | Meaning | First thing to check |
|---|---|---|---|
| stage1 | `X` | no INT 13h extensions | very old BIOS / wrong QEMU drive type |
| stage1 | `R` | reading stage2 failed | image too small, wrong `if=` |
| stage1 | `M` | stage2 magic `S2` missing | image built without stage2 |
| stage2 | `A` | A20 could not be enabled | try another machine type |
| stage2 | `E` | E820 unsupported / empty | BIOS bug |
| stage2 | `R` | disk read failed | manifest LBAs vs image size |
| stage2 | `F` | manifest magic wrong | mkimage version mismatch |
| stage2 | `L` | no `loader` entry | `-payload loader=` missing |
| stage2 | `B` | loader > 256 KiB | raise `LOADER_MAX_SECT` and the memory map |
| stage2 | `C` | CPU lacks long mode | use `-cpu qemu64` |
| stage2 | `K` | loader CRC mismatch | corrupted image / chunked read bug (REG-0001) |
| stage2 | `V` | the `video` payload is corrupt, or VBE would not set a mode it listed (W21) | rebuild with `mkimage -video`; another VGA device |
| loader | `PANIC: <text>` | see the message | `build/smoke-*.log` |
| nucleus | `[nucleus] PANIC: <text>` | see the message | boot_info v2 fields; `make debug` + `add-symbol-file build/nucleus.elf` |

## 6. Serial markers

Markers are the test interface of the boot chain. Format: `[component] message`, and the final success line `BURROW:<STAGE>:OK`. Markers are append-only: once a smoke expect-file lists a marker, changing its text is a breaking change requiring a DEVLOG entry. Current ordered lists: `tests/smoke/expect/boot_to_loader.txt` and `boot_to_nucleus.txt`; `make smoke` checks every expect file against the same boot.

## 7. Nucleus GDT and TSS (W5)

The nucleus replaces stage2's GDT first thing (`nucleus/gdt.c`). The order is dictated by `SYSCALL`/`SYSRET`: `STAR[47:32] = 0x08` gives kernel CS 0x08 / SS 0x10; `STAR[63:48] = 0x18` makes `SYSRET` (64-bit) load CS = 0x18 + 16 = 0x28 and SS = 0x18 + 8 = 0x20, both with RPL 3.

| Selector | Descriptor | Notes |
|---|---|---|
| 0x00 | null | |
| 0x08 | kernel code, 64-bit, DPL 0 | `SEL_KCODE` |
| 0x10 | kernel data, DPL 0 | `SEL_KDATA`; DS/ES/SS |
| 0x18 | null placeholder | only fixes the SYSRET base (32-bit user code would go here) |
| 0x20 | user data, DPL 3 | used as 0x23 |
| 0x28 | user code, 64-bit, DPL 3 | used as 0x2B |
| 0x30 | 64-bit TSS (16 bytes) | `SEL_TSS`, loaded with `ltr` |

TSS: `RSP0` is the current thread's kernel stack top (ring 3 → 0, W9). IST1 = every vector except #DF and NMI; IST2 = #DF; IST3 = NMI; 16 KiB each. FS and GS selectors are 0: their bases come from `FS_BASE`/`GS_BASE` MSRs.
