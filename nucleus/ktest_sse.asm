; ktest_sse.asm - reach xmm0 from C (the nucleus is -mgeneral-regs-only).
bits 64
section .text
global ktest_set_xmm0, ktest_get_xmm0

ktest_set_xmm0:              ; void ktest_set_xmm0(uint64_t v)
    movq xmm0, rdi
    ret

ktest_get_xmm0:              ; uint64_t ktest_get_xmm0(void)
    movq rax, xmm0
    ret
