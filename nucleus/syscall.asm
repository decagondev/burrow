; syscall.asm - the entry points of the SYSCALL instruction (ADR-0004).
;
; What the CPU does on SYSCALL (less than an interrupt):
;   RCX <- return RIP, R11 <- RFLAGS, RFLAGS &= ~FMASK (IF, DF, TF, AC off),
;   CS/SS from STAR, CPL 0. RSP is NOT changed: we are on the caller's stack,
;   which for Go is a small goroutine or g0 stack (PIT-01).
;
; Two entries since W20-S05 (ADR-0011): per-CPU data is behind GS, and a
; user caller's GS base is its own, so the user entry must swapgs before it
; can touch anything, and SYSCALL leaves no register to decide that with.
; LSTAR points at syscall_entry_user while a user thread is this CPU's
; current thread, at syscall_entry_kernel otherwise (sched.c switches it).
;
; Caller classification in the kernel entry, CR3 first (W9): a user thread runs on its process's
; page tables, Go and the nucleus always on the kernel's, so CR3 != the
; kernel's = a user program, whatever its RIP (a process may map code at the
; Go kernel's addresses in its own space). Then RCX inside Go kernel text or
; nucleus text = a ring-0 caller; anything else on the kernel CR3 -> panic.
;
; User path: switch to the thread's kernel stack top (the user RSP is never
; touched), build a struct trap_frame like an interrupt from ring 3 would,
; let nu_user_syscall hand the request to Go (uthread.c) and return through
; trap_resume (IRETQ), so Go can change every register (fork, execve,
; signals: W10). nu_user_syscall makes sure the return RIP is canonical
; (PIT-24) and never returns for a killed thread.
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
USER_CS        equ 0x2B
USER_SS        equ 0x23
SYSCALL_VECTOR equ 0x100            ; outside 0-255: not an interrupt

extern nu_syscall_dispatch          ; long nu_syscall_dispatch(struct syscall_frame *)
extern nu_user_syscall              ; long nu_user_syscall(struct syscall_frame *), uthread.c
extern g_kernel_cr3                 ; shim.c: the kernel PML4, as written to CR3
extern syscall_bad_caller           ; noreturn, panics
extern __text_start, __text_end     ; nucleus/linker.ld
extern trap_resume                  ; trap.asm: restore a struct trap_frame, IRETQ
global syscall_entry_kernel, syscall_entry_user, go_text_lo, go_text_hi

section .data
align 8
go_text_lo: dq 0                    ; set when the Go kernel ELF is mapped (go_load.c)
go_text_hi: dq 0

section .text
syscall_entry_kernel:
    mov [gs:PERCPU_SCRATCH], rax     ; IF=0 (FMASK): the scratch slot is ours
    mov rax, cr3
    cmp rax, [g_kernel_cr3]
    mov rax, [gs:PERCPU_SCRATCH]     ; mov leaves the flags alone
    jne syscall_bad_space
    cmp rcx, [go_text_lo]
    jb .not_go
    cmp rcx, [go_text_hi]
    jb .ring0_caller
.not_go:
    push rax                         ; spare a register on the caller's stack
    lea rax, [rel __text_start]
    cmp rcx, rax
    jb syscall_bad
    lea rax, [rel __text_end]
    cmp rcx, rax
    jae syscall_bad
    pop rax

.ring0_caller:
    mov [gs:PERCPU_SCRATCH], rsp
    cmp rsp, [gs:PERCPU_BOT]
    jb .use_top
    cmp rsp, [gs:PERCPU_TOP]
    jbe .stay                        ; already on this thread's kernel stack
.use_top:
    mov rsp, [gs:PERCPU_TOP]
.stay:
    and rsp, -16
    push qword [gs:PERCPU_SCRATCH]           ; caller RSP
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
    pop qword [gs:PERCPU_SCRATCH]            ; caller RSP (IF=0: scratch is ours)
    push KERNEL_SS
    push qword [gs:PERCPU_SCRATCH]
    push r11
    push KERNEL_CS
    push rcx
    iretq                            ; restores IF from the caller's RFLAGS

syscall_entry_user:
    swapgs                           ; the kernel's GS base, before any gs: access
    mov [gs:PERCPU_SCRATCH], rsp
    mov rsp, [gs:PERCPU_TOP]                 ; empty while the thread runs in ring 3
    push USER_SS                     ; a struct trap_frame, as an interrupt from
    push qword [gs:PERCPU_SCRATCH]           ; ring 3 would leave: SS, RSP,
    push r11                         ; RFLAGS (SYSCALL put them in R11)
    push USER_CS                     ; CS
    push rcx                         ; RIP (SYSCALL put it in RCX)
    push 0                           ; error code
    push SYSCALL_VECTOR              ; "vector": marks a syscall frame
    push rax
    push rbx
    push rcx
    push rdx
    push rsi
    push rdi
    push rbp
    push r8
    push r9
    push r10
    push r11
    push r12
    push r13
    push r14
    push r15                         ; 22 words: RSP stays 16-aligned
    mov rdi, rsp
    sti
    call nu_user_syscall             ; frame.rax = result; Go may have changed any register
    cli
    jmp trap_resume                  ; IRETQ: every register comes from the frame (W10)

syscall_bad_space:                          ; a user CR3 at the kernel entry: LSTAR was not switched
    push rax
syscall_bad:
    pop rax
    mov rsp, [gs:PERCPU_TOP]
    mov rdi, rcx
    call syscall_bad_caller          ; never returns
.halt:
    hlt
    jmp .halt
