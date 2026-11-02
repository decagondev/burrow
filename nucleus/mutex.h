/* mutex.h - a sleeping lock for long nucleus operations (VM commits can zero
 * megabytes; a spinlock would keep interrupts off that long). Built on the
 * futex: 0 = free, 1 = held, 2 = held with waiters. Thread context only,
 * never from an IRQ handler. */
#ifndef NUCLEUS_MUTEX_H
#define NUCLEUS_MUTEX_H
#include <stdint.h>
#include "futex.h"

struct kmutex { uint32_t state; };

#define KMUTEX_INIT { 0 }

static inline void kmutex_lock(struct kmutex *m) {
    uint32_t c = 0;
    if (__atomic_compare_exchange_n(&m->state, &c, 1, 0, __ATOMIC_ACQUIRE, __ATOMIC_RELAXED)) return;
    if (c != 2) c = __atomic_exchange_n(&m->state, 2, __ATOMIC_ACQUIRE);
    while (c != 0) {                                  /* Drepper, "Futexes are tricky", mutex 2 */
        futex_wait(&m->state, 2, 0);
        c = __atomic_exchange_n(&m->state, 2, __ATOMIC_ACQUIRE);
    }
}

static inline void kmutex_unlock(struct kmutex *m) {
    if (__atomic_exchange_n(&m->state, 0, __ATOMIC_RELEASE) == 2) futex_wake(&m->state, 1);
}
#endif
