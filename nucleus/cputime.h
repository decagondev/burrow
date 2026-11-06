/* cputime.h - CPU time per thread and per CPU (W23-S02): every thread's
 * user (ring 3) and kernel nanoseconds, every CPU's user, kernel and idle
 * nanoseconds and switches. Charged at a switch, at every tick, and when a
 * user thread crosses between ring 3 and the kernel; only the CPU running a
 * thread writes its counters (IF=0), and they only grow, so another CPU
 * reads each one whole with a 64-bit load. Docs: docs/04-plan/waves/WAVE-23-proc-virtio.md. */
#ifndef BURROW_NUCLEUS_CPUTIME_H
#define BURROW_NUCLEUS_CPUTIME_H
#include <stdint.h>

struct thread;

struct cputime_cpu {            /* nu_cpu_times's record, 32 bytes per CPU */
    uint64_t user_ns, sys_ns, idle_ns, switches;
};
_Static_assert(sizeof(struct cputime_cpu) == 32, "NUCLEUS_ABI.md: nu_cpu_times");

void cputime_reset(struct thread *t, int in_user);   /* a new thread's counters */
void cputime_switch(struct thread *prev, struct thread *next, uint64_t now_ns);   /* g_sched_lock, IF=0 */
void cputime_tick(struct thread *cur, uint64_t now_ns);                           /* IF=0 */
void cputime_enter_kernel(struct thread *t);   /* a user thread left ring 3 */
void cputime_enter_user(struct thread *t);     /* ... and goes back */
void cputime_thread(struct thread *t, uint64_t *user_ns, uint64_t *sys_ns);
uint32_t cputime_cpus(struct cputime_cpu *out, uint32_t max);   /* CPUs online written */

#endif
