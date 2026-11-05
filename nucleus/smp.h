/* smp.h - the application processors (W20-S05, ADR-0011). smp_init starts
 * every CPU the ACPI MADT lists (INIT, then two STARTUP IPIs into the
 * trampoline in ap.asm) and waits for each to check in. APs run no threads
 * yet (S06): they idle until the BSP hands one a function through its
 * mailbox and a wake IPI. */
#ifndef NUCLEUS_SMP_H
#define NUCLEUS_SMP_H
#include <stdint.h>

#define VEC_WAKE 0xF0   /* wake IPI: an AP's mailbox has work */

void smp_init(void);              /* after syscall_init; prints "[nucleus] smp: N CPUs online" */
uint32_t smp_cpu_count(void);     /* CPUs online, the BSP included */
/* Runs fn(arg) on every online CPU, the BSP (the caller) last, and returns
 * when all have finished. BSP only, threads or boot code with IF either way. */
void smp_run_everywhere(void (*fn)(void *), void *arg);
#endif
