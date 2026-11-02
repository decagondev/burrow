; entry.asm - first instructions of the nucleus. Linked at 0xFFFFFFFF80100000.
; The loader does: mov cr3, <new tables> ; mov rdi, <physmap + 0x1000> ; jmp entry
; Interrupts are off. The loader already zeroed .bss (ELF memsz > filesz), so
; we only switch to our own stack and call C. Must never return.
bits 64
section .text.entry
global nucleus_entry
extern nucleus_main

nucleus_entry:
    cld
    mov rsp, boot_stack_top     ; leave the loader's identity-mapped stack
    xor ebp, ebp                ; end of frame-pointer chain for gdb backtraces
    call nucleus_main           ; rdi = boot_info (physmap address), never returns
.halt:
    cli
    hlt
    jmp .halt

section .bss
align 16
global boot_stack, boot_stack_top
boot_stack:
    resb 16384
boot_stack_top:
