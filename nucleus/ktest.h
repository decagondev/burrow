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

/* ktest_irq.c */
int kt_lapic_self_ipi(void);
#endif
