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

## 3. `struct boot_info` (physical 0x1000)

Defined in `include/burrow/boot_info.h` with `_Static_assert` offsets.

| Offset | Field | Filled by | Meaning |
|---|---|---|---|
| 0x00 | magic u64 | stage2 | `"BOOTINFO"` little-endian |
| 0x08 | version u32 | stage2 | 1 |
| 0x0C | size u32 | stage2 | 0x80 |
| 0x10 | boot_drive u32 | stage2 | BIOS DL, e.g. 0x80 |
| 0x14 | e820_count u32 | stage2 | entries at e820_addr |
| 0x18 | e820_addr u64 | stage2 | 0x2000 |
| 0x20 | manifest_addr u64 | stage2 | 0x0600 |
| 0x28 | loader_base u64 | stage2 | 0x10000 |
| 0x30 | fb_addr u64 | W8 | linear framebuffer, 0 = none |
| 0x38..0x44 | fb_width/height/pitch/bpp | W8 | |
| 0x48 | reserved[7] | — | grow here, bump version |

**Versioning rule:** add fields only into `reserved`; bump `BOOTINFO_VERSION`; the consumer rejects versions it does not know. W4 introduces version 2 (adds module list for nucleus/kernel/initrd physical ranges).

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
| loader | `PANIC: <text>` | see the message | `build/smoke-*.log` |

## 6. Serial markers

Markers are the test interface of the boot chain. Format: `[component] message`, and the final success line `BURROW:<STAGE>:OK`. Markers are append-only: once a smoke expect-file lists a marker, changing its text is a breaking change requiring a DEVLOG entry. Current ordered list: `tests/smoke/expect/boot_to_loader.txt`.
