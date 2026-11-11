/* vdso.h - the vDSO's data page (W25-S02), shared by the nucleus, which
 * writes it (nucleus/vdso.c, from time.c), and the vDSO's code, which every
 * process runs to read the clocks without a system call (user/vdso/vdso.c).
 * The page is mapped read-only into every process just below the code
 * ([vvar] then [vdso]). Readers retry while seq is odd or changes; the
 * nucleus is the only writer. Never holds anything but the clock.
 * Docs: docs/04-plan/waves/WAVE-25-speed.md, NUCLEUS_ABI.md (nu_vdso). */
#ifndef BURROW_VDSO_H
#define BURROW_VDSO_H

#include <stdint.h>

struct vdso_data {
    uint32_t seq;               /* odd while the nucleus writes */
    uint32_t ready;             /* 0 until time_init: readers make the system call */
    uint64_t tsc0;              /* the TSC at monotonic 0 */
    uint64_t ns_mult;           /* monotonic ns = ((tsc - tsc0) * ns_mult) >> 32 */
    uint64_t realtime_base_ns;  /* realtime ns = realtime_base_ns + monotonic ns */
};
_Static_assert(sizeof(struct vdso_data) == 32, "vdso_data layout");

#define VDSO_MAX_CODE_PAGES 4   /* the vDSO image fits in this many pages */

#endif
