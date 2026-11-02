/* futex.h - wait queues keyed by address: what the Go runtime's locks and
 * notes sit on (Linux futex WAIT/WAKE semantics, private, one address space).
 *
 * PIT-23 (lost wake-up): futex_wait compares *addr and enqueues under the
 * bucket lock, and stays T_BLOCKED from that moment; a futex_wake that runs
 * after the compare is guaranteed to find and wake it. */
#ifndef NUCLEUS_FUTEX_H
#define NUCLEUS_FUTEX_H
#include <stdint.h>

/* 0 when woken, -EAGAIN if *addr != expected, -ETIMEDOUT after timeout_ns
 * (0 = wait forever). May also return 0 spuriously: callers re-check. */
long futex_wait(uint32_t *addr, uint32_t expected, uint64_t timeout_ns);
/* Wakes up to count waiters on addr; returns how many. */
long futex_wake(uint32_t *addr, uint32_t count);
#endif
