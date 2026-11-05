; ap.asm - the application processors' trampoline (W20-S05, ADR-0011).
;
; smp.c copies ap_tramp_start..ap_tramp_end to physical TRAMPOLINE_PA and
; fills the data block at its end; a STARTUP IPI with vector
; TRAMPOLINE_PA >> 12 starts an AP here in real mode, CS = 0x0800, IP = 0.
; It goes straight to long mode (CR0.PE and PG in one write): the BSP's CR4
; (PAE, SMEP, SMAP, OSFXSR, OSXSAVE...) and EFER (LME, NXE, SCE), a
; temporary PML4 below 4 GiB that maps the kernel's upper half and the first
; 2 MiB one to one, and then a call to `entry` (ap_main) with RDI = `arg`
; on `stack`. Never returns; never touches memory outside this page and
; that stack. Position-independent: every address is TRAMPOLINE_PA plus an
; offset into the copy. Must match smp.c: TRAMPOLINE_PA, struct ap_boot.
; Intel SDM vol. 3, §9.8 (IA-32e mode initialisation), §8.4.4 (MP init).
;
; Clobbers everything; the AP has no state to keep yet.

TRAMPOLINE_PA equ 0x8000
MSR_EFER      equ 0xC0000080
CODE64        equ 0x08
DATA64        equ 0x10

%define TR(label) (TRAMPOLINE_PA + (label) - ap_tramp_start)

global ap_tramp_start, ap_tramp_end, ap_tramp_data

section .rodata
align 16
bits 16
ap_tramp_start:
    cli
    cld
    mov ax, cs                       ; 0x0800: offsets below are from the page start
    mov ds, ax
    o32 lgdt [gdtr - ap_tramp_start]
    mov eax, [d_cr4 - ap_tramp_start]
    mov cr4, eax
    mov eax, [d_cr3 - ap_tramp_start]   ; below 4 GiB (smp.c checks)
    mov cr3, eax
    mov ecx, MSR_EFER
    mov eax, [d_efer - ap_tramp_start]
    mov edx, [d_efer - ap_tramp_start + 4]
    wrmsr
    mov eax, [d_cr0 - ap_tramp_start]   ; PE, PG, WP, MP...: long mode is active now
    mov cr0, eax
    jmp dword CODE64:TR(long64)         ; a 64-bit CS ends compatibility mode

align 8
bits 64
long64:
    mov ax, DATA64
    mov ds, ax
    mov es, ax
    mov ss, ax
    xor eax, eax
    mov fs, ax
    mov gs, ax                       ; gdt_init_cpu binds GS_BASE later
    mov rsp, [TR(d_stack)]
    mov rdi, [TR(d_arg)]
    mov rax, [TR(d_entry)]
    call rax                         ; ap_main: never returns
.halt:
    cli
    hlt
    jmp .halt

align 16
gdt:
    dq 0
    dq 0x00AF9A000000FFFF            ; 0x08: 64-bit code, DPL 0
    dq 0x00CF92000000FFFF            ; 0x10: data, DPL 0
gdtr:
    dw gdtr - gdt - 1
    dd TR(gdt)

align 8
ap_tramp_data:                       ; struct ap_boot (smp.c), filled per AP
d_cr3:   dq 0
d_cr4:   dq 0
d_efer:  dq 0
d_cr0:   dq 0
d_stack: dq 0
d_entry: dq 0
d_arg:   dq 0
ap_tramp_end:
