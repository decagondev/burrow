; gdt.asm - void gdt_load(const struct gdtr *gdtr, u16 kcode, u16 kdata, u16 tss)
; CS can only be reloaded by a far control transfer: push the selector and the
; return address, then retfq. Clobbers rax. See gdt.h.
bits 64
section .text
global gdt_load

gdt_load:
    lgdt [rdi]
    push rsi                    ; new CS
    lea rax, [rel .reloaded]
    push rax
    retfq                       ; pops RIP, then CS
.reloaded:
    mov ds, dx
    mov es, dx
    mov ss, dx
    xor eax, eax
    mov fs, ax                  ; FS/GS bases come from MSRs (FS_BASE per thread, W5-S10)
    mov gs, ax
    ltr cx
    ret
