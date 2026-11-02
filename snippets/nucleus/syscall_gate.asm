; =============================================================================
; syscall_gate.asm - the ONE entry point for the SYSCALL instruction.
;
; Two kinds of callers execute SYSCALL:
;   (a) the Go kernel (ring 0!) - every Go runtime syscall (mmap, futex, ...)
;   (b) user processes (ring 3) - musl, Doom, ports         (Wave 9+)
;
; What the CPU does on SYSCALL (it does LESS than an interrupt):
;   RCX <- return RIP      R11 <- RFLAGS      RFLAGS &= ~IA32_FMASK (we clear IF)
;   CS/SS <- from IA32_STAR, CPL <- 0
;   RSP is NOT changed.  <-- we are still on the caller's (tiny Go) stack!
;
; Classification rule (ADR-0004): the caller is the Go kernel iff RCX lies in
; the Go kernel's text range. User code cannot run there (supervisor pages),
; so RCX cannot be forged by ring 3.
;
; Return paths:
;   to Go (ring 0): IRETQ. NEVER SYSRET - SYSRET always returns to CPL 3.
;   to user (ring 3): SYSRETQ, but only with a canonical RCX (CVE-2012-0217);
;                     otherwise fall back to IRETQ.
;
; Assemble check: nasm -f elf64 syscall_gate.asm
; =============================================================================
bits 64
default abs

; per-CPU block, addressed through GS (Go never touches GS; it uses FS)
PERCPU_SCRATCH_RSP equ 0x00
PERCPU_KSTACK_TOP  equ 0x08     ; updated by the scheduler on every switch

KERNEL_CS equ 0x08
KERNEL_SS equ 0x10

global syscall_entry
global go_text_lo
global go_text_hi
extern nu_syscall_dispatch        ; long nu_syscall_dispatch(struct syscall_frame *)

section .data
go_text_lo: dq 0                  ; filled from the Go kernel ELF at load time
go_text_hi: dq 0

section .text
syscall_entry:
    cmp rcx, [go_text_lo]
    jb  .from_user
    cmp rcx, [go_text_hi]
    jae .from_user

; ---------------------------------------------------------------- Go caller
.from_go:
    mov [gs:PERCPU_SCRATCH_RSP], rsp
    mov rsp, [gs:PERCPU_KSTACK_TOP]     ; per-THREAD kernel stack: we may block
    push qword [gs:PERCPU_SCRATCH_RSP]  ; caller RSP
    push rcx                            ; caller RIP
    push r11                            ; caller RFLAGS
    call save_and_dispatch              ; RAX = result
    pop r11
    pop rcx
    pop rdx                             ; caller RSP (RDX is clobber-able: Linux ABI)
    ; build an IRETQ frame: SS, RSP, RFLAGS, CS, RIP
    push KERNEL_SS
    push rdx
    push r11
    push KERNEL_CS
    push rcx
    iretq                               ; restores IF from saved RFLAGS atomically

; --------------------------------------------------------------- user caller
.from_user:
    swapgs                              ; GS base <- per-CPU (was user's)
    mov [gs:PERCPU_SCRATCH_RSP], rsp
    mov rsp, [gs:PERCPU_KSTACK_TOP]
    push qword [gs:PERCPU_SCRATCH_RSP]
    push rcx
    push r11
    call save_and_dispatch
    pop r11
    pop rcx
    pop rdx
    ; canonical check: bits 63..47 must all equal bit 47
    mov rsi, rcx
    sar rsi, 47
    jz  .sysret                         ; 0 -> canonical lower half
    ; non-canonical or kernel address: never SYSRET (would #GP in ring 0)
    push 0x23                           ; user SS (RPL 3)  - see GDT layout doc
    push rdx
    push r11
    push 0x2B                           ; user CS (RPL 3)
    push rcx
    swapgs
    iretq
.sysret:
    mov rsp, rdx
    swapgs
    o64 sysret

; save_and_dispatch: push the syscall registers as struct syscall_frame,
; call C, restore callee-saved registers. Linux syscall ABI:
;   nr=RAX, args = RDI RSI RDX R10 R8 R9, return in RAX, clobbers RCX R11.
save_and_dispatch:
    push rax        ; nr
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
    push r15
    mov rdi, rsp                        ; struct syscall_frame *
    sti                                 ; allow preemption while servicing
    call nu_syscall_dispatch
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
    add rsp, 8                          ; drop saved nr; RAX holds the result
    ret
