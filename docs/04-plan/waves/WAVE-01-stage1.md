# W1 Boot Sector  (M1)  — status: done

Goal: the BIOS runs our 512 bytes, which print and load stage2. Reference: `boot/stage1.asm`, `boot/lib16.inc`.

### [x] W1-S01 A boot sector that halts (E01.F01.S01)
Steps: 1) `bits 16`, `org 0x7C00`. 2) `cli; hlt` loop. 3) `times 510-($-$$) db 0` + `dw 0xAA55`. 4) Build with `nasm -f bin`; boot with `make run`.
Test: QEMU does not reboot-loop; `xxd build/stage1.bin | tail -1` ends in `55aa`.
Learn: why 0x7C00, why the signature. OSDev: "Boot Sequence".

### [x] W1-S02 Known state (E01.F01.S01)
Steps: far jump `0x0000:.normalised`; zero DS/ES/SS; `SP=0x7C00`; `cld`; save DL.
Test: gdb `make debug`, break `0x7c00`, step, `info registers` shows segments 0.
Learn: CS:IP normalisation; why the stack sits below the code.

### [x] W1-S03 Serial + screen output (E01.F01.S01)
Files: `boot/lib16.inc` (`serial_init`, `putc`, `print`).
Test: `make run` shows `[stage1] Burrow boot sector`. First smoke marker added.
Learn: 16550 UART registers; BIOS INT 10h AH=0Eh.

### [x] W1-S04 Load stage2 with INT 13h extensions (E01.F01.S02)
Steps: check AH=41h/BX=55AAh; DAP (16 bytes: size, 0, count, offset, segment, LBA64); AH=42h; check `'S2'` magic at `0x7E02`.
Test: marker `[stage1] ok -> stage2`; regression REG-0003 (corrupt magic => `FAIL M`).
Learn: LBA vs CHS.

### [x] W1-S05 Failure codes + fast test exit (E01.F01.S03)
Steps: `fail` prints code, `out 0xF4, 0x11`, halts. Test: `make regress`.
