/* fpu.c - see fpu.h. Intel SDM vol. 3, §13.1.3 (enabling SSE) and ch. 13
 * (XSAVE): CPUID 1 says whether XSAVE and AVX exist, CR4.OSXSAVE allows
 * XSETBV, XCR0 picks the components saved, CPUID 0xD says how big their
 * area is. Plain XSAVE, not XSAVEOPT: the legacy region must always be
 * written, since Go reads it for signal frames (uregs.c). Never enables a
 * component the area has no room for (AVX-512 and the rest stay off). */
#include "fpu.h"
#include "cpu.h"
#include "kprintf.h"
#include "lib_string.h"
#include "panic.h"

#define CR0_MP         (1ull << 1)
#define CR0_EM         (1ull << 2)
#define CR0_TS         (1ull << 3)
#define CR4_OSFXSR     (1ull << 9)
#define CR4_OSXMMEXCPT (1ull << 10)
#define CR4_OSXSAVE    (1ull << 18)
#define CPUID1_ECX_XSAVE (1u << 26)
#define CPUID1_ECX_AVX   (1u << 28)
#define XCR0_AVX       4ull

int g_fpu_xsave;
uint64_t g_fpu_mask;
static uint8_t g_clean[FPU_AREA_BYTES] __attribute__((aligned(64)));

static inline void xsetbv0(uint64_t v) {
    __asm__ volatile("xsetbv" :: "c"(0), "a"((uint32_t)v), "d"((uint32_t)(v >> 32)));
}

/* xsave_setup turns XSAVE on when the CPU has it; the bytes the area needs. */
static uint32_t xsave_setup(uint32_t ecx1) {
    if (!(ecx1 & CPUID1_ECX_XSAVE)) return FPU_LEGACY_BYTES;
    uint32_t a, b, c, d;
    cpuid(0, &a, &b, &c, &d);
    if (a < 0xD) return FPU_LEGACY_BYTES;                 /* no leaf describing the area */
    cpuid(0xD, &a, &b, &c, &d);
    if ((a & XSTATE_X87_SSE) != XSTATE_X87_SSE) return FPU_LEGACY_BYTES;   /* XCR0 cannot hold them */
    uint64_t supported = a;
    write_cr4(read_cr4() | CR4_OSXSAVE);
    g_fpu_mask = XSTATE_X87_SSE | (ecx1 & CPUID1_ECX_AVX && supported & XCR0_AVX ? XCR0_AVX : 0);
    xsetbv0(g_fpu_mask);
    cpuid(0xD, &a, &b, &c, &d);   /* b: the area's size for the components XCR0 now enables */
    if (b > FPU_AREA_BYTES) panic("XSAVE area larger than a thread's FPU area");
    g_fpu_xsave = 1;
    return b;
}

void fpu_init(void) {
    uint32_t a, b, c, d;
    cpuid(1, &a, &b, &c, &d);
    if (!(d & (1u << 24)) || !(d & (1u << 25))) panic("CPU lacks FXSR or SSE");
    write_cr0((read_cr0() & ~(CR0_EM | CR0_TS)) | CR0_MP);
    write_cr4(read_cr4() | CR4_OSFXSR | CR4_OSXMMEXCPT);
    uint32_t bytes = xsave_setup(c);
    __asm__ volatile("fninit");
    uint32_t mxcsr = 0x1F80;                         /* all SSE exceptions masked, round-nearest */
    __asm__ volatile("ldmxcsr %0" :: "m"(mxcsr));
    fpu_save(g_clean);
    if (g_fpu_xsave)
        kprintf("[nucleus] sse ok (xsave per thread: x87 sse%s, %u bytes, mxcsr=%x)\n",
                fpu_has_avx() ? " avx" : "", bytes, mxcsr);
    else
        kprintf("[nucleus] sse ok (fxsave per thread, mxcsr=%x)\n", mxcsr);
}

int fpu_has_avx(void) { return (g_fpu_mask & XCR0_AVX) != 0; }

void fpu_init_area(uint8_t *area) { memcpy(area, g_clean, FPU_AREA_BYTES); }

void fpu_set_legacy(uint8_t *area, const uint8_t *legacy512) {
    memcpy(area, legacy512, FPU_LEGACY_BYTES);
    if (g_fpu_xsave) {
        uint64_t bv;
        memcpy(&bv, area + FPU_XSTATE_BV, 8);
        bv |= XSTATE_X87_SSE;
        memcpy(area + FPU_XSTATE_BV, &bv, 8);
    }
}
