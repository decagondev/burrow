# W3 C Loader  (M3)  — status: done

Reference: `loader/`. Every pure module has host unit tests in `tests/unit/c/`.

### [x] W3-S01 Entry stub + linker script (E03.F01)
`entry.asm` zeroes .bss, aligns stack, calls `loader_main`. `linker.ld`: `.text.entry` first at `0x10000`. Flags in Makefile: `-ffreestanding -mno-red-zone -mgeneral-regs-only` (SSE is not enabled yet!).
Test: `objdump -d build/loader.elf | head` shows `_start` at `0x10000`.

### [x] W3-S02 Freestanding string functions (E03.F01)
`lib_string.c`: memset/memcpy/memcmp/strlen/strncmp. GCC emits calls to these even when you never write them.

### [x] W3-S03 `char_sink` + `kprintf` (E03.F01)
Formatting depends on an interface, not the UART (DIP). Test: `test_kprintf.c` (10 cases incl. INT64_MIN).

### [x] W3-S04 Serial driver as a sink (E03.F01)
Marker `[loader] hello from C`.

### [x] W3-S05 Check boot_info (E03.F02)
Magic, version, size. Marker `[loader] boot_info ok`.

### [x] W3-S06 Memory map summary (E03.F02)
`memmap.c` (pure), require >= 32 MiB. Marker `[loader] memory ok`.

### [x] W3-S07 Manifest validation (E03.F02)
`manifest.c` (pure, 6 tests). Marker `[loader] manifest ok`.

### [x] W3-S08 CRC-32 + self check (E03.F02)
`crc32.c` (check value `0xCBF43926`). Compare RAM image with manifest. Marker `[loader] self-crc ok`.
Rule learned: keep mutable globals in .bss, or the hash changes.

### [x] W3-S09 `block_device` + ATA PIO (E03.F02)
`ata_pio.c` behind `block_device`. Read LBA 1 and compare with the BIOS copy. Marker `[loader] ata ok`.

### [x] W3-S10 ELF64 parser (pure, used in W4) (E03.F03)
`elf64.c` + 5 host tests built from the host's `<elf.h>` (two independent implementations agree).

### [x] W3-S11 Success marker + debug exit
`BURROW:LOADER:OK`, `qemu_exit(0x10)`.
