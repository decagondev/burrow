; entry.asm - first instructions of the loader. Linked at 0x10000.
; stage2 does: mov rdi, &boot_info ; call 0x10000
; We zero .bss (C assumes globals start at 0), align the stack, call C.
bits 64
section .text.entry
global _start
extern loader_main
extern __bss_start
extern __bss_end

_start:
    cld
    push rdi                    ; keep boot_info pointer safe
    mov rdi, __bss_start
    mov rcx, __bss_end
    sub rcx, rdi
    xor eax, eax
    rep stosb                   ; memset(.bss, 0, size)
    pop rdi
    and rsp, -16                ; SysV ABI: 16-byte aligned before 'call'
    call loader_main            ; never returns
.halt:
    cli
    hlt
    jmp .halt
