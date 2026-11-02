; trap.asm - 256 IDT entry stubs and the common trap path. See trap.h.
;
; Every stub pushes an error code (the CPU's, or 0) and its vector, then
; jumps to trap_common, which saves all GPRs (struct trap_frame order) on the
; IST stack and COPIES the frame onto the current thread's kernel stack:
;   - interrupted RSP inside [kstack_bottom, kstack_top]: just below it (a
;     nested trap must not overwrite the outer frame)
;   - anywhere else (Go goroutine stack, user stack): at kstack_top
; The handler then runs on a stack that belongs to this thread alone, so it
; may switch threads; the IST stack is free again for the next trap.
; #DF and NMI (IST2/IST3) skip the copy: they only report and halt.
bits 64
default rel

FRAME_SIZE   equ 22*8
FRAME_RSP    equ 20*8            ; offset of the CPU-pushed RSP in the frame
PERCPU_TOP   equ 0
PERCPU_BOT   equ 8

extern trap_dispatch
extern g_percpu
global trap_stub_table

%macro SAVE_GPRS 0
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
    push r15
%endmacro

%macro RESTORE_GPRS 0
    pop r15
    pop r14
    pop r13
    pop r12
    pop r11
    pop r10
    pop r9
    pop r8
    pop rbp
    pop rdi
    pop rsi
    pop rdx
    pop rcx
    pop rbx
    pop rax
%endmacro

section .text

trap_common:
    cld
    SAVE_GPRS
    mov rsi, rsp                         ; source: frame on the IST stack
    mov rax, [rsi + FRAME_RSP]           ; interrupted RSP
    mov rcx, [g_percpu + PERCPU_TOP]
    cmp rax, [g_percpu + PERCPU_BOT]
    jb .at_top
    cmp rax, rcx
    ja .at_top
    mov rcx, rax                         ; nested: go below the interrupted frame
.at_top:
    and rcx, -16
    sub rcx, FRAME_SIZE
    mov rdi, rcx
    mov rdx, rcx                         ; keep destination
    mov ecx, FRAME_SIZE / 8
    rep movsq
    mov rsp, rdx                         ; now on the thread's kernel stack
    mov rdi, rsp
    call trap_dispatch                   ; may switch threads and come back later
    RESTORE_GPRS
    add rsp, 16                          ; vector, error code
    iretq

trap_common_ist:                         ; #DF, NMI: report in place, never return
    cld
    SAVE_GPRS
    mov rdi, rsp
    and rsp, -16
    call trap_dispatch
.halt:
    cli
    hlt
    jmp .halt

; ---- stubs ------------------------------------------------------------------
; Vectors where the CPU pushes an error code itself.
%define HAS_ERR(v) ((v) == 8 || (v) == 10 || (v) == 11 || (v) == 12 || (v) == 13 || (v) == 14 || (v) == 17 || (v) == 21 || (v) == 29 || (v) == 30)

%assign v 0
%rep 256
trap_stub_ %+ v:
%if !HAS_ERR(v)
    push 0
%endif
    push v
%if v == 2 || v == 8
    jmp trap_common_ist
%else
    jmp trap_common
%endif
%assign v v + 1
%endrep

section .rodata
align 8
trap_stub_table:
%assign v 0
%rep 256
    dq trap_stub_ %+ v
%assign v v + 1
%endrep
