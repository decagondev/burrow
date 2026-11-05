/* smp.h - the application processors (W20-S05, ADR-0011). smp_init starts
 * every CPU the ACPI MADT lists (INIT, then two STARTUP IPIs into the
 * trampoline in ap.asm) and waits for each to check in. Each AP then runs
 * threads from the one run queue (S06, sched.c); its idle thread also runs
 * a function the BSP puts in its mailbox (and sends a wake IPI for). */
#ifndef NUCLEUS_SMP_H
#define NUCLEUS_SMP_H
#include <stdint.h>

#define VEC_WAKE 0xF0   /* wake IPI: a mailbox has work, or a thread a CPU may run */

void smp_init(void);              /* after syscall_init; prints "[nucleus] smp: N CPUs online" */
uint32_t smp_cpu_count(void);     /* CPUs online, the BSP included */
/* Runs fn(arg) on every online CPU, the BSP (the caller) last, and returns
 * when all have finished. BSP only, threads or boot code with IF either way. */
void smp_run_everywhere(void (*fn)(void *), void *arg);
/* An idle thread, IF=0: runs this CPU's mailbox function if there is one. */
void smp_mailbox_run(void);
#endif
