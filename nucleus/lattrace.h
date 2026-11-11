/* lattrace.h - the steps of a user system call's round trip (W25-S01):
 * while one call number is traced, each request of it is stamped with the
 * TSC at the gate (nu_user_syscall), when Go takes it (uthread_wait), when
 * Go answers (uthread_reply), when the thread runs again (post_and_wait)
 * and on its way out to ring 3; the four intervals are summed here. Pure:
 * no io.h or cpu.h (host-tested); the stamps are taken in uthread.c.
 * Never traces more than one call number at a time.
 * Docs: docs/04-plan/waves/WAVE-25-speed.md, NUCLEUS_ABI.md (nu_lattrace). */
#ifndef BURROW_LATTRACE_H
#define BURROW_LATTRACE_H

#include <stdint.h>

enum {
    LAT_GATE, LAT_TAKEN, LAT_REPLIED, LAT_RESUMED,   /* stamps, in order */
    LAT_STAMPS
};
#define LAT_STEPS 4   /* gate->taken, taken->replied, replied->resumed, resumed->out */

struct lat_stats {
    int      on;                 /* tracing nr */
    uint64_t nr;
    uint64_t count;              /* requests summed */
    uint64_t skipped;            /* requests whose stamps were out of order */
    uint64_t sum[LAT_STEPS];     /* TSC ticks per step, over count requests */
};

void lat_start(struct lat_stats *s, uint64_t nr);
void lat_stop(struct lat_stats *s);
int  lat_wanted(const struct lat_stats *s, uint64_t nr);
/* Adds one request: stamps[LAT_STAMPS] and the stamp on the way out. A
 * request whose stamps are missing or out of order is counted as skipped. */
void lat_record(struct lat_stats *s, const uint64_t stamps[LAT_STAMPS], uint64_t out);

#endif
