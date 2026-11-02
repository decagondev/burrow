/* ktest.h - in-kernel tests for what the host cannot test (real traps, page
 * tables, timers, threads). A "ktest" boot module (a text file on the disk
 * image, tests/ktest/<suite>.txt) names the tests to run, whitespace
 * separated. Each prints "[ktest] <name> ok"; any failure panics, so a suite
 * either ends with BURROW:KTEST:OK (exit 33) or stops loudly (exit 35).
 *
 * Adding a test: write `int kt_<name>(void)` returning 0 in a ktest_*.c
 * file, declare it below, add it to the table in ktest.c. */
#ifndef NUCLEUS_KTEST_H
#define NUCLEUS_KTEST_H
#include <stdint.h>

#define KT_CHECK(cond) do { if (!(cond)) { ktest_fail(__FILE__, __LINE__, #cond); return 1; } } while (0)

void ktest_fail(const char *file, int line, const char *what);
/* Runs the tests named in text[0..len). Never returns. */
void ktest_run(const char *text, uint64_t len) __attribute__((noreturn));

/* ktest_trap.c */
int kt_trap_ud_fixup(void);
int kt_trap_gp_fixup(void);
int kt_trap_pf_fixup(void);
int kt_trap_unhandled_ud(void);   /* deliberately panics: REG-0005 */

/* ktest_mm.c */
int kt_pmm_alloc_all_free_all(void);
int kt_vmm_map_write_unmap(void);
int kt_vmm_protect_readonly(void);
int kt_vmm_kernel_wx(void);
int kt_vm_reserve_commit(void);
int kt_vm_reserve_hint(void);

/* ktest_irq.c */
int kt_lapic_self_ipi(void);
int kt_time_tick_rate(void);
int kt_irq_wait_pit(void);
int kt_irq_pending_before_wait(void);
int kt_irq_masked_until_ack(void);
int kt_irq_syscall(void);

/* ktest_sched.c */
int kt_sched_ping_pong(void);
int kt_sched_preempts(void);
int kt_sched_sleep_and_fs_base(void);
int kt_sched_stack_guard(void);
int kt_sse_state_per_thread(void);

/* ktest_sync.c */
int kt_spinlock_counter(void);
int kt_futex_ping_pong(void);
int kt_futex_timeout_and_eagain(void);

/* ktest_syscall.c */
int kt_syscall_getpid(void);
int kt_syscall_write(void);
int kt_shim_unknown_once(void);
int kt_madvise_dontneed_zeroes(void);
int kt_nu_bootinfo_mmio(void);

/* ktest_user.c */
int kt_uspace_map_translate(void);
int kt_uspace_kernel_half_shared(void);

/* ktest_uthread.c */
int kt_uthread_syscalls(void);
int kt_uthread_faults(void);
int kt_uthread_preempt_kill(void);
int kt_uthread_noncanonical_return(void);
int kt_uthread_fs_base(void);
#endif
