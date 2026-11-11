/* vdso.h - the nucleus's side of the vDSO (W25-S02): a data page the clock
 * is published in (include/burrow/vdso.h) and frames holding the vDSO's
 * code (vdso_image.asm), both mapped into every process by the Go kernel
 * (nu_vdso tells it where they are). Never frees them.
 * Docs: docs/04-plan/waves/WAVE-25-speed.md. */
#ifndef NUCLEUS_VDSO_H
#define NUCLEUS_VDSO_H

#include <stdint.h>

void vdso_init(void);   /* after frames_init and before time_init */
/* Publishes the clock (time.c: at time_init and at every realtime change). */
void vdso_update(uint64_t tsc0, uint64_t ns_mult, uint64_t realtime_base_ns);
/* nu_vdso: buf = {data phys, code bytes, code pages, page phys x pages}. */
long vdso_info(uint64_t *buf);

#endif
