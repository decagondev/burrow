/* percpu.h - per-CPU state (one CPU today). trap.asm reads the first two
 * fields by fixed offset: keep them first and in this order. */
#ifndef NUCLEUS_PERCPU_H
#define NUCLEUS_PERCPU_H
#include <stdint.h>

struct thread;

struct percpu {
    uint64_t kstack_top;      /* +0: current thread's kernel stack, highest address */
    uint64_t kstack_bottom;   /* +8: lowest usable address of that stack            */
    struct thread *current;   /* +16                                                */
};
_Static_assert(__builtin_offsetof(struct percpu, kstack_top) == 0, "trap.asm offset");
_Static_assert(__builtin_offsetof(struct percpu, kstack_bottom) == 8, "trap.asm offset");

extern struct percpu g_percpu;
#endif
