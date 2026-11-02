; syscall.asm - the ONE entry point of the SYSCALL instruction (ADR-0004).
;
; What the CPU does on SYSCALL (less than an interrupt):
;   RCX <- return RIP, R11 <- RFLAGS, RFLAGS &= ~FMASK (IF, DF, TF, AC off),
;   CS/SS from STAR, CPL 0. RSP is NOT changed: we are on the caller's stack,
;   which for Go is a small goroutine or g0 stack (PIT-01).
;
; Caller classification: RCX inside Go kernel text or nucleus text = a ring-0
; caller (both are supervisor-only, so ring 3 cannot produce such an RCX).
; Anything else would be a user program: not supported until W9 -> panic.
;
; Stack: the thread's kernel stack, like trap.asm: just below RSP if the
; caller already runs on it (nucleus code), else its top (Go code).
;
; Return to ring 0 with IRETQ, never SYSRET (PIT-02: SYSRET forces CPL 3).
; Linux ABI: RAX = result; RCX and R11 are clobbered (here: RIP and RFLAGS,
; exactly what Linux leaves in them); every other register is preserved.
bits 64
default rel

PERCPU_TOP     equ 0
PERCPU_BOT     equ 8
PERCPU_SCRATCH equ 24
KERNEL_CS      equ 0x08
KERNEL_SS      equ 0x10

extern g_percpu
extern nu_syscall_dispatch          ; long nu_syscall_dispatch(struct syscall_frame *)
extern syscall_bad_caller           ; noreturn, panics
extern __text_start, __text_end     ; nucleus/linker.ld
global syscall_entry, go_text_lo, go_text_hi

section .data
align 8
go_text_lo: dq 0                    ; set when the Go kernel ELF is mapped (go_load.c)
go_text_hi: dq 0

section .text
syscall_entry:
    cmp rcx, [go_text_lo]
    jb .not_go
    cmp rcx, [go_text_hi]
    jb .ring0_caller
.not_go:
    push rax                         ; spare a register on the caller's stack
    lea rax, [rel __text_start]
    cmp rcx, rax
    jb .bad
    lea rax, [rel __text_end]
    cmp rcx, rax
    jae .bad
    pop rax

.ring0_caller:
    mov [g_percpu + PERCPU_SCRATCH], rsp
    cmp rsp, [g_percpu + PERCPU_BOT]
    jb .use_top
    cmp rsp, [g_percpu + PERCPU_TOP]
    jbe .stay                        ; already on this thread's kernel stack
.use_top:
    mov rsp, [g_percpu + PERCPU_TOP]
.stay:
    and rsp, -16
    push qword [g_percpu + PERCPU_SCRATCH]   ; caller RSP
    push rcx                         ; caller RIP
    push r11                         ; caller RFLAGS
    push rax                         ; struct syscall_frame starts here (top: nr)
    push rdi
    push rsi
    push rdx
    push r10
    push r8
    push r9
    push rbx
    push rbp
    push r12
    push r13
    push r14
    push r15                         ; 16 pushes: RSP stays 16-aligned for the call
    mov rdi, rsp
    sti                              ; preemptible while the syscall is serviced
    call nu_syscall_dispatch         ; RAX = result
    cli
    pop r15
    pop r14
    pop r13
    pop r12
    pop rbp
    pop rbx
    pop r9
    pop r8
    pop r10
    pop rdx
    pop rsi
    pop rdi
    add rsp, 8                       ; saved nr; RAX holds the result
    pop r11                          ; caller RFLAGS (Linux leaves it in R11 too)
    pop rcx                          ; caller RIP    (Linux leaves it in RCX too)
    pop qword [g_percpu + PERCPU_SCRATCH]    ; caller RSP (IF=0: scratch is ours)
    push KERNEL_SS
    push qword [g_percpu + PERCPU_SCRATCH]
    push r11
    push KERNEL_CS
    push rcx
    iretq                            ; restores IF from the caller's RFLAGS

.bad:
    pop rax
    mov rsp, [g_percpu + PERCPU_TOP]
    mov rdi, rcx
    call syscall_bad_caller          ; never returns
.halt:
    hlt
    jmp .halt
