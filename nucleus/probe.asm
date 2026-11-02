; probe.asm - instructions that are ALLOWED to fault. Each faulting
; instruction has a .fixup entry {fault rip, resume rip}; trap_dispatch
; resumes there and records the fault in trap_last. See trap.h.
bits 64
default rel
section .text
global probe_read64, probe_write64, probe_ud2

; int probe_read64(uint64_t addr, uint64_t *out)
probe_read64:
.fault:
    mov rax, [rdi]
    mov [rsi], rax
    xor eax, eax
    ret
.resume:
    mov eax, -1
    ret

; int probe_write64(uint64_t addr, uint64_t value)
probe_write64:
.fault:
    mov [rdi], rsi
    xor eax, eax
    ret
.resume:
    mov eax, -1
    ret

; int probe_ud2(void)
probe_ud2:
.fault:
    ud2
.resume:
    mov eax, -1
    ret

section .fixup progbits alloc noexec nowrite align=8
    dq probe_read64.fault,  probe_read64.resume
    dq probe_write64.fault, probe_write64.resume
    dq probe_ud2.fault,     probe_ud2.resume
