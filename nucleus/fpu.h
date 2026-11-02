/* fpu.h - x87/SSE state. The nucleus's own C code is built with
 * -mgeneral-regs-only and never touches xmm/x87, so traps need not save
 * them; only a thread switch does (Go code uses SSE freely, W6).
 * Eager save: fxsave/fxrstor on every switch (512 bytes, simple, cheap). */
#ifndef NUCLEUS_FPU_H
#define NUCLEUS_FPU_H
#include <stdint.h>

#define FX_AREA_BYTES 512u

void fpu_init(void);                               /* CR0/CR4 bits, clean template */
void fpu_init_area(uint8_t *area);                 /* new thread: clean state */
static inline void fpu_save(uint8_t *area)    { __asm__ volatile("fxsave64 (%0)" :: "r"(area) : "memory"); }
static inline void fpu_restore(uint8_t *area) { __asm__ volatile("fxrstor64 (%0)" :: "r"(area) : "memory"); }
#endif
