; ktest_syscall.asm - uint64_t ktest_syscall_clobbers(void)
; Loads a distinct pattern into every register the Linux syscall ABI says
; survives SYSCALL, calls getpid (39), and returns a bit mask of the registers
; that changed (0 = all preserved). RAX, RCX, R11 are allowed to change.
bits 64
section .text
global ktest_syscall_clobbers

%macro CHECK 2                   ; CHECK reg, bit
    mov rax, 0x1111000000000000 + %2
    cmp %1, rax
    je %%ok
    bts r11, %2
%%ok:
%endmacro

ktest_syscall_clobbers:
    push rbx
    push rbp
    push r12
    push r13
    push r14
    push r15
    mov rdi, 0x1111000000000000 + 0
    mov rsi, 0x1111000000000000 + 1
    mov rdx, 0x1111000000000000 + 2
    mov r10, 0x1111000000000000 + 3
    mov r8,  0x1111000000000000 + 4
    mov r9,  0x1111000000000000 + 5
    mov rbx, 0x1111000000000000 + 6
    mov rbp, 0x1111000000000000 + 7
    mov r12, 0x1111000000000000 + 8
    mov r13, 0x1111000000000000 + 9
    mov r14, 0x1111000000000000 + 10
    mov r15, 0x1111000000000000 + 11
    mov eax, 39
    syscall
    cmp rax, 1                   ; getpid answers 1
    mov r11, 0
    je .ret_ok
    bts r11, 63
.ret_ok:
    CHECK rdi, 0
    CHECK rsi, 1
    CHECK rdx, 2
    CHECK r10, 3
    CHECK r8,  4
    CHECK r9,  5
    CHECK rbx, 6
    CHECK rbp, 7
    CHECK r12, 8
    CHECK r13, 9
    CHECK r14, 10
    CHECK r15, 11
    mov rax, r11
    pop r15
    pop r14
    pop r13
    pop r12
    pop rbp
    pop rbx
    ret
