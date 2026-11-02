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

; void enter_nucleus(uint64_t cr3, uint64_t entry, uint64_t boot_info_virt)
; Switch to the nucleus address space and jump; never returns. This code runs
; from identity-mapped low memory, which the new tables still map (PIT-21).
; Interrupts stay off; the nucleus sets its own stack first thing.
section .text
global enter_nucleus
enter_nucleus:
    mov cr3, rdi                ; rdi = PML4 physical address
    mov rdi, rdx                ; SysV arg 1 for nucleus_main: &boot_info (physmap)
    jmp rsi                     ; rsi = ELF entry (higher half)
