# W2 Stage2 to Long Mode  (M2)  — status: done

Reference: `boot/stage2.asm`. Each slice adds one routine and one marker. Commit after each.

### [x] W2-S01 Stage2 header and hello (E02.F01)
`jmp short` + `db 'S2'`; reuse `lib16.inc`. Marker `[stage2] hello from stage2`.

### [x] W2-S02 Enable A20 (E02.F01)
`a20_check` (wrap test at 0x500 vs FFFF:0510), then BIOS 2401h, then port 0x92 (never set bit 0!). Marker `[stage2] a20 ok`.
Learn: why the 8086 wrapped at 1 MiB.

### [x] W2-S03 E820 memory map (E02.F01)
Loop INT 15h EAX=E820h, 24-byte entries at `0x2000`, skip zero-length, stop at EBX=0 or carry. Marker `[stage2] e820 ok`.
Test (W3): loader prints the table; unit tests cover `memmap.c`.

### [x] W2-S04 Read and check the manifest (E02.F01)
Generic `disk_read(EAX=lba, CX=count, DX:BX=dest)`; `repe cmpsb` against `BRWMNFST`. Marker `[stage2] manifest ok`. Regression REG-0002.

### [x] W2-S05 Find and load the loader (E02.F01)
Linear search of 40-byte entries for the 16-byte name `loader`; load in 64-sector chunks advancing the segment by `0x800`. Marker `[stage2] loader loaded`.
Learn: real-mode segment arithmetic (paragraphs).

### [x] W2-S06 CPUID long-mode check + boot_info (E02.F03, E02.F01)
Leaf `0x80000001` EDX bit 29. Zero then fill 128 bytes at `0x1000` (offsets from `boot_abi.inc`).

### [x] W2-S07 GDT and protected mode (E02.F02)
5 descriptors (null, code32, data32, code64, data); `lgdt`; CR0.PE; far jump. Marker `[stage2] protected mode ok` (printed by `print32`).
Learn: descriptor bit layout (`0x00CF9A000000FFFF` decoded in DEBUGGING.md).

### [x] W2-S08 Verify the loader's CRC before running it (E02.F03, BUG-0003)
`crc32_ieee` in 32-bit asm. Marker `[stage2] loader crc ok`. Regression REG-0001.

### [x] W2-S09 Identity paging and long mode (E02.F02)
PML4[0]→PDPT[0]→PD with 512 x 2 MiB pages (flags `0x83`); CR4.PAE; CR3; EFER.LME; CR0.PG; far jump to 64-bit code. Marker `[stage2] long mode ok -> loader`.
Learn: why paging is mandatory in long mode. Debug: a triple fault here = check `-d int,cpu_reset` (DEBUGGING.md).

### [x] W2-S10 Call the loader (E02.F02)
`RSP=0x80000`, `RDI=0x1000`, `call 0x10000`.
