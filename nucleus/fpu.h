/* fpu.h - x87/SSE/AVX state. The nucleus's own C code is built with
 * -mgeneral-regs-only and never touches xmm/ymm/x87, so traps need not
 * save them; only a thread switch does (Go code uses SSE freely, W6, and
 * AVX when the CPU offers it, W20-S03). Eager save on every switch: XSAVE
 * and XRSTOR of the components XCR0 enables (x87, SSE, AVX) when the CPU
 * has XSAVE, else FXSAVE and FXRSTOR. The area's first 512 bytes are
 * FXSAVE's layout either way: that is what nu_uthread_regs hands Go. */
#ifndef NUCLEUS_FPU_H
#define NUCLEUS_FPU_H
#include <stdint.h>

#define FPU_AREA_BYTES   1024u   /* x87 + SSE (512) + XSAVE header (64) + AVX (256) = 832 */
#define FPU_LEGACY_BYTES 512u    /* the FXSAVE part */
#define FPU_XSTATE_BV    512u    /* the XSAVE header's first field: which components hold state */
#define XSTATE_X87_SSE   3ull

extern int g_fpu_xsave;          /* fpu.c: 1 when switches use XSAVE */
extern uint64_t g_fpu_mask;      /* the XCR0 components saved */

void fpu_init(void);                               /* CR0/CR4/XCR0, clean template */
void fpu_init_cpu(void);                           /* on an AP: XCR0, fninit, MXCSR (W20-S05) */
void fpu_init_area(uint8_t *area);                 /* new thread: clean state */
int fpu_has_avx(void);                             /* AVX state is kept per thread */
/* set_legacy writes FXSAVE-format registers into an area (a fork child, a
 * sigreturn) so that the next restore loads them: XRSTOR would reset
 * components the header calls unused. */
void fpu_set_legacy(uint8_t *area, const uint8_t *legacy512);

static inline void fpu_save(uint8_t *area) {
    if (g_fpu_xsave)
        __asm__ volatile("xsave64 (%0)" :: "r"(area), "a"((uint32_t)g_fpu_mask), "d"((uint32_t)(g_fpu_mask >> 32)) : "memory");
    else
        __asm__ volatile("fxsave64 (%0)" :: "r"(area) : "memory");
}

static inline void fpu_restore(uint8_t *area) {
    if (g_fpu_xsave)
        __asm__ volatile("xrstor64 (%0)" :: "r"(area), "a"((uint32_t)g_fpu_mask), "d"((uint32_t)(g_fpu_mask >> 32)) : "memory");
    else
        __asm__ volatile("fxrstor64 (%0)" :: "r"(area) : "memory");
}
#endif
