; hello.asm - the first user program (W9-S05): no libc, two Linux system
; calls. write(1, msg, len) then exit_group(0). Runs in ring 3; both calls
; travel SYSCALL -> nucleus gate -> request queue -> the Go kernel (proc).
bits 64
default rel

SYS_write      equ 1
SYS_exit_group equ 231

section .rodata
msg:    db "hello from ring 3", 10
len     equ $ - msg

section .text
global _start
_start:
    mov eax, SYS_write
    mov edi, 1                  ; stdout
    lea rsi, [msg]
    mov edx, len
    syscall
    mov eax, SYS_exit_group
    xor edi, edi                ; status 0
    syscall
    ud2                         ; never reached
