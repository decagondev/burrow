/* tlb.c - see tlb.h. One shootdown at a time (g_shoot_lock): the sender
 * writes the range, marks each target CPU's shoot_pending, sends VEC_SHOOT
 * and waits until every mark is cleared. Targets: every other online CPU
 * for an upper-half range (every address space shares it), else the CPUs
 * whose percpu.cr3 is the space's root. The page-table writes happen before
 * the sender reads those fields (a full fence), and the scheduler updates a
 * CPU's field right after its mov to CR3, which flushes the old space; so a
 * CPU left out cannot hold a stale entry. Never waits with a target unable
 * to answer: spinlock loops call tlb_poll. Intel SDM vol. 3 §4.10.5; ADR-0011. */
#include "tlb.h"
#include "percpu.h"
#include "smp.h"
#include "apic.h"
#include "trap.h"
#include "spinlock.h"
#include "paging.h"
#include "cpu.h"

struct shoot_req { uint64_t va, len_bytes; };

static struct shoot_req g_req;           /* stable while g_shoot_lock is held */
static struct spinlock g_shoot_lock = SPINLOCK_INIT("tlb");

/* A lower-half range of many pages: reload CR3 (drops every non-global
 * entry). The upper half page by page: a CR3 reload keeps global entries. */
static void flush_local(uint64_t va, uint64_t len_bytes) {
    if (!(va >> 63) && len_bytes / PAGE_4K > TLB_FULL_PAGES) {
        write_cr3(read_cr3());
        return;
    }
    for (uint64_t off = 0; off < len_bytes; off += PAGE_4K) invlpg(va + off);
}

void tlb_poll(void) {
    struct percpu *c = this_cpu();
    if (!__atomic_load_n(&c->shoot_pending, __ATOMIC_ACQUIRE)) return;
    flush_local(g_req.va, g_req.len_bytes);
    __atomic_store_n(&c->shoot_pending, 0, __ATOMIC_RELEASE);
}

static void on_shoot(struct trap_frame *f) {
    (void)f;
    tlb_poll();
    lapic_eoi();
}

void tlb_init(void) { trap_set_handler(VEC_SHOOT, on_shoot); }

void tlb_set_loaded(uint64_t root) { __atomic_store_n(&this_cpu()->cr3, root, __ATOMIC_SEQ_CST); }

/* g_shoot_lock held, IF=0: marks and interrupts the targets; returns them. */
static uint64_t send(const struct percpu *me, uint64_t root, int upper) {
    uint64_t sent = 0;
    __atomic_thread_fence(__ATOMIC_SEQ_CST);   /* the entries' writes before the reads of cr3 */
    for (uint32_t i = 0, n = smp_cpu_count(); i < n; i++) {
        struct percpu *c = &g_cpus[i];
        if (c == me || !__atomic_load_n(&c->online, __ATOMIC_ACQUIRE)) continue;
        if (!upper && __atomic_load_n(&c->cr3, __ATOMIC_SEQ_CST) != root) continue;
        __atomic_store_n(&c->shoot_pending, 1, __ATOMIC_RELEASE);
        lapic_send_ipi((uint8_t)c->apic_id, VEC_SHOOT);
        sent |= 1ull << i;
    }
    return sent;
}

void tlb_shootdown(uint64_t root, uint64_t va, uint64_t len_bytes) {
    int upper = (va >> 63) != 0;
    uint64_t f = irq_save();                     /* stays on this CPU throughout */
    struct percpu *me = this_cpu();
    if (upper || __atomic_load_n(&me->cr3, __ATOMIC_RELAXED) == root) flush_local(va, len_bytes);
    if (smp_cpu_count() > 1) {
        spin_lock(&g_shoot_lock);                /* polls: another sender may be waiting for us */
        g_req.va = va;
        g_req.len_bytes = len_bytes;
        uint64_t sent = send(me, root, upper);
        for (uint32_t i = 0; sent; i++, sent >>= 1)
            if (sent & 1)
                while (__atomic_load_n(&g_cpus[i].shoot_pending, __ATOMIC_ACQUIRE)) cpu_pause();
        spin_unlock(&g_shoot_lock);
    }
    irq_restore(f);
}
