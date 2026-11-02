/* fpu.c - see fpu.h. Intel SDM vol. 3, §13.1.3 (enabling SSE). */
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

static uint8_t g_clean[FX_AREA_BYTES] __attribute__((aligned(16)));

void fpu_init(void) {
    uint32_t a, b, c, d;
    cpuid(1, &a, &b, &c, &d);
    if (!(d & (1u << 24)) || !(d & (1u << 25))) panic("CPU lacks FXSR or SSE");
    write_cr0((read_cr0() & ~(CR0_EM | CR0_TS)) | CR0_MP);
    write_cr4(read_cr4() | CR4_OSFXSR | CR4_OSXMMEXCPT);
    __asm__ volatile("fninit");
    uint32_t mxcsr = 0x1F80;                         /* all SSE exceptions masked, round-nearest */
    __asm__ volatile("ldmxcsr %0" :: "m"(mxcsr));
    fpu_save(g_clean);
    kprintf("[nucleus] sse ok (fxsave per thread, mxcsr=%x)\n", mxcsr);
}

void fpu_init_area(uint8_t *area) { memcpy(area, g_clean, FX_AREA_BYTES); }
