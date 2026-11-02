; switch.asm - the only place one thread's stack becomes another's.
;
; void switch_to(struct thread *prev, struct thread *next)
;   Saves the SysV callee-saved registers on prev's stack, stores RSP in
;   prev->rsp (offset 0), loads next->rsp, restores, returns into next.
;   Caller-saved registers need no saving: switch_to is an ordinary call.
;   Interrupts must be OFF (sched.c guarantees it).
;
; thread_start: where a new thread's first switch_to "returns" to. Its
;   stack was built by sched.c: r12 = fn, r13 = arg.
bits 64
section .text
global switch_to, thread_start
extern thread_bootstrap

switch_to:
    push rbp
    push rbx
    push r12
    push r13
    push r14
    push r15
    mov [rdi], rsp
    mov rsp, [rsi]
    pop r15
    pop r14
    pop r13
    pop r12
    pop rbx
    pop rbp
    ret

thread_start:
    mov rdi, r12
    mov rsi, r13
    and rsp, -16                ; SysV: aligned before the call
    call thread_bootstrap       ; never returns (calls thread_exit)
    ud2
