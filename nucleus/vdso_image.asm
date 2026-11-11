; vdso_image.asm - the vDSO's code (W25-S02): user/vdso/vdso.c linked by the
; Makefile into a small shared object ($(BUILD)/vdso/vdso.so, VDSO_SO), carried
; here as read-only data; vdso.c copies it into frames that the Go kernel maps
; into every process. Never executed in the kernel. Clobbers nothing: data only.
; Docs: docs/04-plan/waves/WAVE-25-speed.md.

section .rodata
align 16
global vdso_image
global vdso_image_end
vdso_image:
    incbin VDSO_SO
vdso_image_end:
