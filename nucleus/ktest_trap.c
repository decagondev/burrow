/* ktest_trap.c - the trap path end to end: expected faults are caught by
 * fixups with the right vector, error code and CR2; an unexpected one panics. */
#include "ktest.h"
#include "trap.h"

#define UNMAPPED_VA 0x0000400000000000ull   /* canonical, lower half, never mapped in W5 */

int kt_trap_ud_fixup(void) {
    uint64_t before = trap_last.count;
    KT_CHECK(probe_ud2() == -1);
    KT_CHECK(trap_last.count == before + 1);
    KT_CHECK(trap_last.vector == VEC_UD);
    return 0;
}

int kt_trap_gp_fixup(void) {
    uint64_t v = 0;
    KT_CHECK(probe_read64(0x0000800000000000ull, &v) == -1);   /* non-canonical -> #GP */
    KT_CHECK(trap_last.vector == VEC_GP);
    return 0;
}

int kt_trap_pf_fixup(void) {
    uint64_t v = 0;
    KT_CHECK(probe_read64(UNMAPPED_VA + 0x123, &v) == -1);
    KT_CHECK(trap_last.vector == VEC_PF);
    KT_CHECK(trap_last.cr2 == UNMAPPED_VA + 0x123);
    KT_CHECK((trap_last.error & 1) == 0);          /* P=0: page not present */
    KT_CHECK(probe_write64(UNMAPPED_VA, 1) == -1);
    KT_CHECK((trap_last.error & 2) != 0);          /* W=1: it was a write */
    return 0;
}

int kt_trap_unhandled_ud(void) {
    __asm__ volatile("ud2");                       /* no fixup: must panic with [trap] #UD */
    return 1;
}
