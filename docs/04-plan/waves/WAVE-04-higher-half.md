# W4 Higher-Half Hand-off  (M4)  — status: next

Goal: the loader loads a C nucleus ELF linked at `0xFFFFFFFF80100000`, builds real page tables (identity + physmap + kernel), passes boot_info v2, and the nucleus prints `BURROW:NUCLEUS:OK`.

Pre-reading: MEMORY_MAP.md §2, `snippets/nucleus/linker.ld`, OSDev "Higher Half x86 Bare Bones".

### [x] W4-S01 Nucleus "hello" ELF (E03.F04.S04)
Files: `nucleus/entry.asm`, `nucleus/main.c`, `nucleus/linker.ld` (from snippets), Makefile rule `build/nucleus.elf` with `-mcmodel=kernel -mno-red-zone -mgeneral-regs-only -ffreestanding`.
Steps: entry sets its own stack (16 KiB in .bss), calls `nucleus_main(bi)`, which writes to COM1 directly (copy `serial.c`).
Test: `readelf -lW build/nucleus.elf` shows VirtAddr `0xffffffff80100000`, PhysAddr `0x100000`. `mkimage -payload nucleus=` adds it; `-inspect` lists it.

### [x] W4-S02 Loader physical allocator (E03.F03.S01)
File: `loader/palloc.c` (pure: takes the E820 array + reserved ranges, hands out 4 KiB frames above 1 MiB, never inside reserved ranges or images).
Test: host unit tests with synthetic maps (overlaps, holes, exhaustion).

### [x] W4-S03 Load a manifest entry into RAM (E03.F03.S02)
File: `loader/image.c`: `image_read(block_device*, entry, dest)` + CRC check.
Test: marker `[loader] nucleus image crc ok`; regression REG-0004 (corrupt nucleus byte => `PANIC: nucleus CRC`).

### [x] W4-S04 Copy PT_LOAD segments (E03.F03.S03)
Use `elf64_segment_at`; copy `filesz` bytes to `paddr`, zero to `memsz`; reject overlaps with loader/boot data (pure check, unit-tested).
Test: marker `[loader] nucleus loaded entry=ffffffff80100000`.

### [x] W4-S05 Page-table builder (pure) (E03.F04.S01)
File: `loader/paging.c`: `pt_map(ctx, va, pa, size, flags)` where `ctx` provides `alloc_frame()` and `phys_to_ptr()` (DIP: on the host they are malloc + identity).
Test: host tests walk the produced tables and assert translations for 4 KiB/2 MiB pages, flags, and non-canonical rejection.

### [x] W4-S06 Build the three mappings (E03.F04.S02)
Identity first 1 GiB (temporary), physmap of all usable RAM at `0xFFFF800000000000` (2 MiB pages), nucleus segments with W^X flags. Enable NX (EFER.NXE) before using bit 63.

### [x] W4-S07 boot_info v2 (E03.F04.S03)
Add `modules[]` (name, phys start, size) and `physmap_base` inside `reserved`; bump version to 2; update BOOT_PROTOCOL.md, both asserts. Stage2 keeps writing v1 fields; the loader upgrades to v2.

### [ ] W4-S08 Jump (E03.F04.S04)
Load CR3, jump to entry with `RDI = physmap_base + 0x1000`. Nucleus checks magic/version 2 and prints `BURROW:NUCLEUS:OK`; append markers to a new expect file `boot_to_nucleus.txt`; make it the default smoke.

### [ ] W4-S09 CI + diagram validation (E00.F01.S03, E00.F03.S02)
`.github/workflows/ci.yml` running `make test` on ubuntu-latest (apt: nasm qemu-system-x86 golang), plus Mermaid parse step.

Exit: M4 demo. DEVLOG wave summary.
