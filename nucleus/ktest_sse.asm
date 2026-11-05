; ktest_sse.asm - reach xmm0 from C (the nucleus is -mgeneral-regs-only).
bits 64
section .text
global ktest_set_xmm0, ktest_get_xmm0, ktest_set_ymm0_hi, ktest_get_ymm0_hi

ktest_set_xmm0:              ; void ktest_set_xmm0(uint64_t v)
    movq xmm0, rdi
    ret

ktest_get_xmm0:              ; uint64_t ktest_get_xmm0(void)
    movq rax, xmm0
    ret

; W20-S03: ymm0's upper half (AVX), which only XSAVE keeps per thread
ktest_set_ymm0_hi:           ; void ktest_set_ymm0_hi(uint64_t v)
    vmovq xmm1, rdi
    vinsertf128 ymm0, ymm0, xmm1, 1
    ret

ktest_get_ymm0_hi:           ; uint64_t ktest_get_ymm0_hi(void)
    vextractf128 xmm1, ymm0, 1
    vmovq rax, xmm1
    ret
