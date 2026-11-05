/* ktest_smp.c - the application processors and per-CPU state (W20-S05,
 * ADR-0011). Suite "smp" boots with -smp 4: every CPU the MADT lists is
 * online, each finds its own struct percpu through GS, and all of them add
 * to one counter under a spinlock without losing an increment. In suite
 * "user": a ring-3 program that reloads GS cannot move the kernel's
 * per-CPU base (the swapgs on every ring-3 entry and exit). */
#include "pat.h"
#include "ktest.h"
#include "ktest_nu.h"
#include "smp.h"
#include "percpu.h"
#include "acpi.h"
#include "apic.h"
#include "spinlock.h"

#define ADDS_PER_CPU 200000u

static struct spinlock g_count_lock = SPINLOCK_INIT("ktest-smp");
static volatile uint64_t g_count;
static uint64_t g_seen_mask;   /* bit per CPU index that ran, set atomically */
static int g_bad_cpu;          /* a CPU whose GS or APIC id did not match */

int kt_smp_cpus_online(void) {
    const struct acpi_info *m = acpi_machine();
    KT_CHECK(m != 0);
    KT_CHECK(m->ncpus > 1);                     /* the suite runs on -smp 4 */
    KT_CHECK(smp_cpu_count() == m->ncpus);
    for (uint32_t i = 0; i < smp_cpu_count(); i++) KT_CHECK(g_cpus[i].online);
    return 0;
}

static void whoami(void *arg) {
    (void)arg;
    struct percpu *c = this_cpu();
    if (c->self != c || c != &g_cpus[c->index] || c->apic_id != lapic_read(LAPIC_ID) >> 24)
        __atomic_store_n(&g_bad_cpu, 1, __ATOMIC_RELAXED);
    __atomic_fetch_or(&g_seen_mask, 1ull << c->index, __ATOMIC_RELAXED);
}

/* REG-0008: smp.c put a "jmp $" where the trampoline starts, so no AP checked
 * in; this must fail loudly, not boot on fewer CPUs in silence. */
int kt_smp_broken_trampoline(void) {
    KT_CHECK(smp_cpu_count() == acpi_machine()->ncpus);
    return 0;
}

int kt_smp_percpu(void) {
    g_seen_mask = 0;
    g_bad_cpu = 0;
    smp_run_everywhere(whoami, 0);
    KT_CHECK(!g_bad_cpu);
    KT_CHECK(g_seen_mask == (1ull << smp_cpu_count()) - 1);
    return 0;
}

static void add_many(void *arg) {
    (void)arg;
    for (uint32_t i = 0; i < ADDS_PER_CPU; i++) {
        uint64_t f = spin_lock_irqsave(&g_count_lock);
        g_count = g_count + 1;                  /* a read and a write: racy without the lock */
        spin_unlock_irqrestore(&g_count_lock, f);
    }
}

int kt_smp_spin_counter(void) {
    g_count = 0;
    smp_run_everywhere(add_many, 0);
    KT_CHECK(g_count == (uint64_t)ADDS_PER_CPU * smp_cpu_count());
    return 0;
}

/* mov gs, 0x23 (base 0) in ring 3, a syscall, a busy loop the timer
 * interrupts, then exit(7): the kernel's GS must survive both entries. */
int kt_uthread_gs_reload(void) {
    static const uint8_t code[] = {
        0xB8, 0x23, 0, 0, 0,                  /* mov eax, 0x23          */
        0x8E, 0xE8,                           /* mov gs, eax            */
        0xB8, 39, 0, 0, 0, 0x0F, 0x05,        /* mov eax, 39 ; syscall  */
        0xB9, 0x00, 0x2D, 0x31, 0x01,         /* mov ecx, 20000000      */
        0xE2, 0xFE,                           /* loop $                 */
        0xBF, 7, 0, 0, 0,                     /* mov edi, 7             */
        0xB8, 60, 0, 0, 0, 0x0F, 0x05,        /* mov eax, 60 ; syscall  */
        0xEB, 0xFE,                           /* jmp $                  */
    };
    struct percpu *before = this_cpu();
    struct uproc p;
    KT_CHECK(uproc_new(&p, code, sizeof code, CODE_VA) == 0);
    long tid = nu(0x1020, p.as, CODE_VA, STACK_TOP, 0, 0);
    KT_CHECK(tid > 0);
    KT_CHECK(next_req() == 1 && g_req[0] == (uint64_t)tid && g_req[1] == 39);
    KT_CHECK(this_cpu() == before);
    KT_CHECK(nu(0x1022, tid, 41, 0, 0, 0) == 0);
    KT_CHECK(next_req() == 1 && g_req[1] == 60 && g_req[2] == 7);
    KT_CHECK(this_cpu() == before && before->self == before);
    KT_CHECK(rdmsr(MSR_KERNEL_GS_BASE) == 0);   /* the user's base, parked while in ring 0 */
    KT_CHECK(nu(0x1023, tid, 0, 0, 0, 0) == 0);
    KT_CHECK(uproc_free(&p) == 0);
    return 0;
}

/* W21-S04: every CPU loaded the same PAT (PIT-65). */
static uint64_t g_pat_seen[SMP_MAX_CPUS];

static void read_pat(void *arg) { (void)arg; g_pat_seen[this_cpu()->index] = rdmsr(MSR_PAT); }

int kt_smp_pat_everywhere(void) {
    smp_run_everywhere(read_pat, 0);
    for (uint32_t i = 1; i < smp_cpu_count(); i++) KT_CHECK(g_pat_seen[i] == g_pat_seen[0]);
    KT_CHECK(((g_pat_seen[0] >> (8 * PAT_WC_INDEX)) & 0xFF) == PAT_TYPE_WC);
    return 0;
}
