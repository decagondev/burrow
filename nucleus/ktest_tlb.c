/* ktest_tlb.c - TLB shootdown and user threads on every CPU (W20-S07,
 * ADR-0011), suite "smp" on -smp 4. A kernel thread on another CPU keeps
 * writing a page while main write-protects or unmaps it: once the call
 * returns, every later write must fault, and the frame must not change
 * (without shootdown the writer's CPU keeps its writable entry). User
 * threads compute on the other CPUs and are killed there at once. */
#include "ktest.h"
#include "ktest_nu.h"
#include "sched.h"
#include "smp.h"
#include "percpu.h"
#include "vmm.h"
#include "frames.h"
#include "boot.h"
#include "trap.h"
#include "time.h"

#define TEST_VA        0x0000500000000000ull   /* lower half of the kernel's space: unused in ktests */
#define WAIT_NS        1000000000ull
#define SETTLE_NS      1000000ull
#define WATCH_NS       20000000ull
#define KILL_BUDGET_NS 50000000ull
#define SPINNERS       3u

static uint32_t bits(uint64_t v) { uint32_t n = 0; for (; v; v &= v - 1) n++; return n; }   /* no libgcc */

static volatile uint64_t g_writes, g_writer_cpu;
static volatile int g_faulted;

static void writer(void *arg) {
    (void)arg;
    for (uint64_t i = 1;; i++) {
        g_writer_cpu = this_cpu()->index;
        if (probe_write64(TEST_VA, i) != 0) { g_faulted = 1; return; }
        g_writes = i;
    }
}

static int wait_for(volatile uint64_t *v, uint64_t at_least) {
    uint64_t until = time_monotonic_ns() + WAIT_NS;
    while (*v < at_least) if (time_monotonic_ns() > until) return 0;
    return 1;
}

/* unmap = 0: write-protect the page; 1: unmap it (the frame stays ours). */
static int writer_loses_page(int unmap) {
    uint64_t pa = frame_alloc_zeroed();
    KT_CHECK(pa && vm_map(vmm_kernel(), TEST_VA, pa, PAGE_4K, PTE_W | PTE_NX) == PT_OK);
    g_writes = 0, g_writer_cpu = 0, g_faulted = 0;
    struct thread *w = thread_create("tlb-writer", writer, 0);
    KT_CHECK(w);
    KT_CHECK(wait_for(&g_writes, 1000));
    KT_CHECK(g_writer_cpu != this_cpu()->index);   /* the entry to shoot is on another CPU */
    if (unmap) KT_CHECK(vm_unmap(vmm_kernel(), TEST_VA, PAGE_4K, 0) == PT_OK);
    else KT_CHECK(vm_protect(vmm_kernel(), TEST_VA, PAGE_4K, PTE_NX) == PT_OK);   /* read-only */
    time_busy_wait_ns(SETTLE_NS);
    volatile uint64_t *frame = phys_to_virt(pa);
    uint64_t v0 = *frame;
    time_busy_wait_ns(WATCH_NS);
    KT_CHECK(*frame == v0);                         /* no write landed after the call returned */
    thread_join(w);                                 /* it ends at its first fault */
    KT_CHECK(g_faulted);
    if (!unmap) KT_CHECK(vm_unmap(vmm_kernel(), TEST_VA, PAGE_4K, 0) == PT_OK);
    frame_free(pa);
    return 0;
}

int kt_smp_shootdown_protect(void) { return writer_loses_page(0); }
int kt_smp_shootdown_unmap(void) { return writer_loses_page(1); }

/* Three ring-3 threads spin ("jmp $") on the CPUs main (CPU 0) leaves free;
 * each is killed promptly although it never enters the kernel by itself. */
int kt_smp_user_threads_everywhere(void) {
    static const uint8_t code[] = { 0xEB, 0xFE };   /* jmp $ */
    uint32_t n = smp_cpu_count() - 1 < SPINNERS ? smp_cpu_count() - 1 : SPINNERS;
    struct uproc p;
    long tid[SPINNERS];
    KT_CHECK(n > 0 && uproc_new(&p, code, sizeof code, CODE_VA) == 0);
    for (uint32_t i = 0; i < n; i++) KT_CHECK((tid[i] = nu(0x1020, p.as, CODE_VA, STACK_TOP, 0, 0)) > 0);
    time_busy_wait_ns(WATCH_NS);                     /* main keeps CPU 0 */
    uint64_t cpus = 0;
    for (uint32_t i = 0; i < n; i++) {
        struct thread *t = thread_find((uint32_t)tid[i]);
        KT_CHECK(t && t->state == T_RUNNING);
        cpus |= 1ull << t->cpu;
    }
    KT_CHECK(!(cpus & 1) && bits(cpus) == n);   /* one per other CPU */
    for (uint32_t i = 0; i < n; i++) {
        uint64_t t0 = time_monotonic_ns();
        KT_CHECK(nu(0x1023, tid[i], 0, 0, 0, 0) == 0);
        KT_CHECK(time_monotonic_ns() - t0 < KILL_BUDGET_NS);
    }
    KT_CHECK(uproc_free(&p) == 0);
    return 0;
}
