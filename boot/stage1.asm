; =============================================================================
; stage1.asm - Burrow boot sector (MBR). Exactly 512 bytes.
;
; Job (and ONLY this job - Single Responsibility):
;   1. Get into a known state (segments, stack, direction flag).
;   2. Say hello on COM1 and the screen so we know we are alive.
;   3. Load stage2 (32 sectors from LBA 2) to 0x0000:0x7E00 using INT 13h
;      extended reads (LBA, no CHS math).
;   4. Jump to stage2 with DL = boot drive.
;
; Build:  nasm -f bin -I boot/ boot/stage1.asm -o build/stage1.bin
; =============================================================================
bits 16
org 0x7C00

%include "boot_abi.inc"

start:
    ; Some BIOSes jump to 07C0:0000, others to 0000:7C00. Normalise CS to 0
    ; with a far jump so that our 'org 0x7C00' labels are correct.
    jmp 0x0000:.normalised
.normalised:
    cli
    xor ax, ax
    mov ds, ax
    mov es, ax
    mov ss, ax
    mov sp, STAGE1_ADDR          ; stack grows down from just below us
    sti
    cld                          ; string ops go forwards

    mov [boot_drive], dl         ; BIOS hands us the boot drive in DL

    call serial_init
    mov si, msg_hello
    call print

    ; --- Are INT 13h extensions available? (AH=41h, BX=55AAh)
    mov ah, 0x41
    mov bx, 0x55AA
    mov dl, [boot_drive]
    int 0x13
    jc  err_no_ext
    cmp bx, 0xAA55
    jne err_no_ext

    ; --- Read stage2 with AH=42h using the Disk Address Packet below.
    mov si, dap
    mov ah, 0x42
    mov dl, [boot_drive]
    int 0x13
    jc  err_read

    ; --- Sanity check: stage2 starts with a 2-byte jmp then the magic 'S2'.
    cmp word [STAGE2_ADDR + 2], 'S2'
    jne err_magic

    mov si, msg_jump
    call print
    mov dl, [boot_drive]
    jmp 0x0000:STAGE2_ADDR

; ---------------------------------------------------------------------------
; Error paths: print a one-letter code and halt forever.
;   X = no INT13 extensions, R = disk read failed, M = stage2 magic wrong
; ---------------------------------------------------------------------------
err_no_ext: mov al, 'X'
            jmp fail
err_read:   mov al, 'R'
            jmp fail
err_magic:  mov al, 'M'
fail:
    mov si, msg_err
    push ax
    call print
    pop ax
    call putc
    mov al, 0x11                 ; QEMU debug-exit failure (no-op on hardware)
    out DEBUG_EXIT_PORT, al
.halt:
    cli
    hlt
    jmp .halt

%include "lib16.inc"          ; serial_init, putc, print (shared with stage2)

; ---------------------------------------------------------------------------
; Data
; ---------------------------------------------------------------------------
boot_drive: db 0

align 4
dap:                             ; INT 13h AH=42h Disk Address Packet
    db 0x10                      ; size of packet
    db 0                         ; reserved
    dw STAGE2_SECTORS            ; sectors to read
    dw STAGE2_ADDR               ; buffer offset
    dw 0x0000                    ; buffer segment
    dq STAGE2_LBA                ; starting LBA

msg_hello: db "[stage1] Burrow boot sector", 13, 10, 0
msg_jump:  db "[stage1] ok -> stage2", 13, 10, 0
msg_err:   db 13, 10, "[stage1] FAIL ", 0

; Pad to 510 bytes and add the boot signature the BIOS looks for.
times 510 - ($ - $$) db 0
dw 0xAA55
