; crash.asm - a user program that dereferences NULL (W9-S05). The page
; fault must end this process only: the Go kernel logs it, the console's
; `run` reports status 139 (SIGSEGV), and everything else keeps running.
bits 64
default rel

SYS_write equ 1

section .rodata
msg:    db "about to dereference NULL", 10
len     equ $ - msg

section .text
global _start
_start:
    mov eax, SYS_write
    mov edi, 1
    lea rsi, [msg]
    mov edx, len
    syscall
    mov rax, [0]                ; page 0 is never mapped
    ud2
