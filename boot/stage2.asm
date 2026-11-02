; =============================================================================
; stage2.asm - Burrow second stage. Loaded by stage1 at 0x7E00 (max 16 KiB).
;
; Pipeline (each step is a small routine you can write and test on its own):
;   REAL MODE (16-bit, BIOS available)
;     1. enable_a20       - make addresses above 1 MiB real
;     2. detect_memory    - INT 15h E820 -> table at 0x2000
;     3. read_manifest    - disk LBA 1 -> 0x0600, check magic
;     4. find_loader      - look up the "loader" entry in the manifest
;     5. load_loader      - INT 13h ext reads -> 0x10000
;     6. check_long_mode  - CPUID: does this CPU do x86-64?
;     7. fill_bootinfo    - struct boot_info at 0x1000
;   PROTECTED MODE (32-bit, no BIOS from here on)
;     8. build_page_tables - identity-map the first 1 GiB with 2 MiB pages
;     9. enable long mode  - PAE, CR3, EFER.LME, CR0.PG
;   LONG MODE (64-bit)
;    10. call loader_entry(rdi = &boot_info)
;
; Build: nasm -f bin -I boot/ boot/stage2.asm -o build/stage2.bin
; =============================================================================
bits 16
org 0x7E00

%include "boot_abi.inc"

CODE32_SEL equ 0x08
DATA32_SEL equ 0x10
CODE64_SEL equ 0x18
DATA64_SEL equ 0x20

    jmp short s2_start
    db 'S2'                      ; magic checked by stage1 (offset 2)

s2_start:
    xor ax, ax
    mov ds, ax
    mov es, ax
    mov [boot_drive], dl

    mov si, msg_hello
    call print

    call enable_a20
    call detect_memory
    call read_manifest
    call find_loader
    call load_loader
    call check_long_mode
    call fill_bootinfo

    mov si, msg_pm
    call print

    ; ---- Real mode -> Protected mode -------------------------------------
    cli                          ; no IDT for 32/64-bit yet: interrupts stay OFF
    lgdt [gdt_desc]
    mov eax, cr0
    or  eax, 1                   ; CR0.PE
    mov cr0, eax
    jmp CODE32_SEL:pm_entry      ; far jump reloads CS -> now executing 32-bit

; =============================================================================
; 1. A20
; =============================================================================
enable_a20:
    call a20_check
    jne .on
    mov ax, 0x2401               ; method 1: ask the BIOS
    int 0x15
    call a20_check
    jne .on
    in  al, 0x92                 ; method 2: "fast A20" via system control port
    or  al, 2
    and al, 0xFE                 ; never set bit 0 (that resets the machine!)
    out 0x92, al
    call a20_check
    jne .on
    mov al, 'A'
    jmp fail
.on:
    mov si, msg_a20
    jmp print                    ; tail call

; a20_check: ZF=1 -> A20 DISABLED (memory wraps), ZF=0 -> enabled.
; Writes different bytes to 0x000500 and 0x100500 (FFFF:0510). If A20 is off,
; both addresses are the same byte.
a20_check:
    push ds
    push es
    xor ax, ax
    mov es, ax
    not ax
    mov ds, ax                   ; DS = 0xFFFF
    mov di, 0x0500
    mov si, 0x0510
    mov al, [es:di]
    push ax
    mov al, [ds:si]
    push ax
    mov byte [es:di], 0x00
    mov byte [ds:si], 0xFF
    cmp byte [es:di], 0xFF       ; did writing high memory change low memory?
    pop ax
    mov [ds:si], al
    pop ax
    mov [es:di], al              ; mov/pop do not touch flags
    pop es
    pop ds
    ret

; =============================================================================
; 2. Memory map via INT 15h, EAX=E820h
; =============================================================================
detect_memory:
    mov di, E820_ADDR
    xor ebx, ebx                 ; continuation value, 0 = start
    xor bp, bp                   ; entry count
.next:
    mov eax, 0xE820
    mov edx, 0x534D4150          ; 'SMAP'
    mov ecx, 24
    mov dword [es:di + 20], 1    ; pre-set ACPI "valid" bit for 20-byte BIOSes
    int 0x15
    jc  .end                     ; carry = end of list (or error if first call)
    cmp eax, 0x534D4150
    jne .bad
    jcxz .skip                   ; zero-length record from buggy BIOS
    mov ecx, [es:di + 8]
    or  ecx, [es:di + 12]
    jz  .skip                    ; region length 0 -> ignore
    inc bp
    add di, 24
    cmp bp, E820_MAX
    jae .end
.skip:
    test ebx, ebx
    jnz .next
.end:
    test bp, bp
    jz  .bad
    mov [e820_count], bp
    mov si, msg_e820
    jmp print
.bad:
    mov al, 'E'
    jmp fail

; =============================================================================
; Disk helper: read CX sectors at LBA EAX into DX:BX (segment:offset)
; =============================================================================
disk_read:
    mov [dap.count], cx
    mov [dap.offset], bx
    mov [dap.segment], dx
    mov [dap.lba], eax
    mov dword [dap.lba + 4], 0
    mov si, dap
    mov ah, 0x42
    mov dl, [boot_drive]
    int 0x13
    jc  .err
    ret
.err:
    mov al, 'R'
    jmp fail

; =============================================================================
; 3/4. Manifest
; =============================================================================
read_manifest:
    mov eax, MANIFEST_LBA
    mov cx, 1
    xor dx, dx
    mov bx, MANIFEST_ADDR
    call disk_read
    mov si, MANIFEST_ADDR + MF_MAGIC
    mov di, manifest_magic
    mov cx, 8
    repe cmpsb
    je  .ok
    mov al, 'F'
    jmp fail
.ok:
    mov si, msg_manifest
    jmp print

find_loader:
    mov si, MANIFEST_ADDR + MF_ENTRIES
    mov cx, [MANIFEST_ADDR + MF_COUNT]
    jcxz .missing
.cmp:
    push cx
    push si
    mov di, name_loader
    mov cx, 16                   ; compare the full NUL-padded name field
    repe cmpsb
    pop si
    pop cx
    je  .found
    add si, ME_SIZE
    loop .cmp
.missing:
    mov al, 'L'
    jmp fail
.found:
    mov eax, [si + ME_LBA]
    mov [loader_lba], eax
    mov eax, [si + ME_SECTORS]
    cmp eax, LOADER_MAX_SECT
    ja  .too_big
    mov [loader_sectors], eax
    mov eax, [si + ME_SIZE_BYTES]          ; low dword is plenty (<= 256 KiB)
    mov [loader_bytes], eax
    mov eax, [si + ME_CRC32]
    mov [loader_crc], eax
    ret
.too_big:
    mov al, 'B'
    jmp fail

; =============================================================================
; 5. Load the loader in 64-sector (32 KiB) chunks. Real-mode segments let us
;    address up to 1 MiB; each chunk advances the segment by 64*512/16 = 0x800.
; =============================================================================
load_loader:
    mov eax, [loader_lba]
    mov ecx, [loader_sectors]
    mov dx, LOADER_SEG
.chunk:
    test ecx, ecx
    jz  .done
    mov ebx, ecx
    cmp ebx, 64
    jbe .sized
    mov ebx, 64
.sized:
    push eax
    push ecx
    push edx
    push ebx
    mov cx, bx
    xor bx, bx
    call disk_read               ; DX:0000 <- CX sectors from LBA EAX
    pop ebx
    pop edx
    pop ecx
    pop eax
    add eax, ebx                 ; next LBA
    sub ecx, ebx                 ; sectors left
    shl bx, 5                    ; sectors * 32 paragraphs
    add dx, bx                   ; next segment
    jmp .chunk
.done:
    mov si, msg_loaded
    jmp print

; =============================================================================
; 6. Long mode support? CPUID leaf 0x80000001, EDX bit 29 (LM).
; =============================================================================
check_long_mode:
    mov eax, 0x80000000
    cpuid
    cmp eax, 0x80000001
    jb  .no
    mov eax, 0x80000001
    cpuid
    test edx, 1 << 29
    jz  .no
    ret
.no:
    mov al, 'C'
    jmp fail

; =============================================================================
; 7. struct boot_info (see include/burrow/boot_info.h)
; =============================================================================
fill_bootinfo:
    mov di, BOOTINFO_ADDR
    mov cx, BI_STRUCT_SIZE
    xor al, al
    rep stosb                    ; zero everything first: unknown = 0
    mov dword [BOOTINFO_ADDR + BI_MAGIC],         BOOTINFO_MAGIC_LO
    mov dword [BOOTINFO_ADDR + BI_MAGIC + 4],     BOOTINFO_MAGIC_HI
    mov dword [BOOTINFO_ADDR + BI_VERSION],       BOOTINFO_VERSION
    mov dword [BOOTINFO_ADDR + BI_SIZE],          BI_STRUCT_SIZE
    movzx eax, byte [boot_drive]
    mov dword [BOOTINFO_ADDR + BI_BOOT_DRIVE],    eax
    movzx eax, word [e820_count]
    mov dword [BOOTINFO_ADDR + BI_E820_COUNT],    eax
    mov dword [BOOTINFO_ADDR + BI_E820_ADDR],     E820_ADDR
    mov dword [BOOTINFO_ADDR + BI_MANIFEST_ADDR], MANIFEST_ADDR
    mov dword [BOOTINFO_ADDR + BI_LOADER_BASE],   LOADER_ADDR
    ret

; =============================================================================
; Failure: print code and halt. AL = one-letter code (see docs/BOOT_ERRORS).
; =============================================================================
fail:
    push ax
    mov si, msg_fail
    call print
    pop ax
    call putc
    mov al, 0x11                 ; QEMU debug-exit failure (no-op on hardware)
    out DEBUG_EXIT_PORT, al
.halt:
    cli
    hlt
    jmp .halt

%include "lib16.inc"

; =============================================================================
; 32-BIT PROTECTED MODE
; =============================================================================
bits 32
pm_entry:
    mov ax, DATA32_SEL
    mov ds, ax
    mov es, ax
    mov fs, ax
    mov gs, ax
    mov ss, ax
    mov esp, STAGE1_ADDR

    mov esi, msg_pm_ok
    call print32

    ; ---- Verify before you execute: CRC-32 of the loader vs the manifest ----
    mov esi, LOADER_ADDR
    mov ecx, [loader_bytes]
    call crc32_ieee
    cmp eax, [loader_crc]
    je  .crc_ok
    mov esi, msg_crc_bad
    call print32
    mov al, 0x11                 ; QEMU debug-exit: failure (status 35)
    out DEBUG_EXIT_PORT, al
.dead:
    hlt
    jmp .dead
.crc_ok:
    mov esi, msg_crc_ok
    call print32

    ; ---- 8. Page tables: PML4[0] -> PDPT[0] -> PD[0..511] (2 MiB each) ----
    mov edi, PML4_ADDR
    xor eax, eax
    mov ecx, (3 * 4096) / 4
    rep stosd                    ; zero PML4, PDPT, PD

    mov dword [PML4_ADDR], PDPT_ADDR | 0x3      ; present | writable
    mov dword [PDPT_ADDR], PD_ADDR   | 0x3
    mov edi, PD_ADDR
    mov eax, 0x83                ; present | writable | PS (2 MiB page)
    mov ecx, 512
.pd:
    mov [edi], eax
    mov dword [edi + 4], 0
    add eax, 0x200000
    add edi, 8
    loop .pd

    ; ---- 9. Enter long mode ---------------------------------------------
    mov eax, cr4
    or  eax, 1 << 5              ; CR4.PAE
    mov cr4, eax
    mov eax, PML4_ADDR
    mov cr3, eax
    mov ecx, 0xC0000080          ; IA32_EFER
    rdmsr
    or  eax, 1 << 8              ; EFER.LME
    wrmsr
    mov eax, cr0
    or  eax, 1 << 31             ; CR0.PG -> long mode becomes active
    mov cr0, eax
    jmp CODE64_SEL:lm_entry

; crc32_ieee - ESI = data, ECX = length -> EAX = CRC-32 (IEEE, reflected).
; Same algorithm as common/crc32.c and Go's hash/crc32.ChecksumIEEE.
crc32_ieee:
    mov eax, 0xFFFFFFFF
.byte:
    test ecx, ecx
    jz  .done
    xor al, [esi]
    inc esi
    mov edx, 8
.bit:
    shr eax, 1                   ; CF = bit shifted out
    jnc .no_xor
    xor eax, 0xEDB88320
.no_xor:
    dec edx
    jnz .bit
    dec ecx
    jmp .byte
.done:
    not eax
    ret

; print32 - ESI -> NUL-terminated string, serial only (BIOS is gone)
print32:
    lodsb
    test al, al
    jz  .done
    mov bl, al
    mov dx, COM1 + 5
.wait:
    in  al, dx
    test al, 0x20
    jz  .wait
    mov dx, COM1
    mov al, bl
    out dx, al
    jmp print32
.done:
    ret

; =============================================================================
; 64-BIT LONG MODE
; =============================================================================
bits 64
lm_entry:
    mov ax, DATA64_SEL
    mov ds, ax
    mov es, ax
    mov ss, ax
    xor ax, ax
    mov fs, ax
    mov gs, ax
    mov rsp, LOADER_STACK_TOP    ; 16-byte aligned

    mov rsi, msg_lm_ok
    call print64

    mov rdi, BOOTINFO_ADDR       ; SysV ABI: first argument in RDI
    mov rax, LOADER_ADDR
    call rax                     ; loader_entry(struct boot_info *)

    mov rsi, msg_returned        ; the loader should never return
    call print64
.halt:
    cli
    hlt
    jmp .halt

print64:
    lodsb
    test al, al
    jz  .done
    mov bl, al
    mov dx, COM1 + 5
.wait:
    in  al, dx
    test al, 0x20
    jz  .wait
    mov dx, COM1
    mov al, bl
    out dx, al
    jmp print64
.done:
    ret

; =============================================================================
; Data
; =============================================================================
align 8
gdt:
    dq 0                         ; 0x00 null descriptor (required)
    dq 0x00CF9A000000FFFF        ; 0x08 32-bit code, base 0, limit 4 GiB
    dq 0x00CF92000000FFFF        ; 0x10 32-bit data
    dq 0x00AF9A000000FFFF        ; 0x18 64-bit code (L=1, D=0)
    dq 0x00CF92000000FFFF        ; 0x20 data (mostly ignored in long mode)
gdt_end:
gdt_desc:
    dw gdt_end - gdt - 1
    dd gdt

align 4
dap:
    db 0x10, 0
.count:   dw 0
.offset:  dw 0
.segment: dw 0
.lba:     dq 0

boot_drive:     db 0
e820_count:     dw 0
loader_lba:     dd 0
loader_sectors: dd 0
loader_bytes:   dd 0
loader_crc:     dd 0

manifest_magic: db "BRWMNFST"
name_loader:    db "loader", 0, 0, 0, 0, 0, 0, 0, 0, 0, 0   ; 16 bytes

msg_hello:    db "[stage2] hello from stage2", 13, 10, 0
msg_a20:      db "[stage2] a20 ok", 13, 10, 0
msg_e820:     db "[stage2] e820 ok", 13, 10, 0
msg_manifest: db "[stage2] manifest ok", 13, 10, 0
msg_loaded:   db "[stage2] loader loaded", 13, 10, 0
msg_pm:       db "[stage2] entering protected mode", 13, 10, 0
msg_pm_ok:    db "[stage2] protected mode ok", 13, 10, 0
msg_crc_ok:   db "[stage2] loader crc ok", 13, 10, 0
msg_crc_bad:  db "[stage2] FAIL K loader crc mismatch", 13, 10, 0
msg_lm_ok:    db "[stage2] long mode ok -> loader", 13, 10, 0
msg_returned: db "[stage2] FAIL loader returned", 13, 10, 0
msg_fail:     db 13, 10, "[stage2] FAIL ", 0

; stage2 must fit its 32-sector slot. NASM errors out if this goes negative.
times (STAGE2_SECTORS * 512) - ($ - $$) db 0
