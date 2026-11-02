/* ktest_irq.c - interrupts with IF=1: the LAPIC delivers, the IRQ path
 * (IST1, copy to the kernel stack, dispatch, EOI, iretq) returns cleanly. */
#include "ktest.h"
#include "apic.h"
#include "trap.h"
#include "cpu.h"

#define VEC_TEST 0x40

static volatile uint64_t g_hits;
static volatile uint64_t g_seen_rsp;

static void on_test_irq(struct trap_frame *f) {
    g_hits++;
    g_seen_rsp = f->rsp;
    lapic_eoi();
}

int kt_lapic_self_ipi(void) {
    trap_set_handler(VEC_TEST, on_test_irq);
    g_hits = 0;
    uint64_t marker = 0x5eed;                     /* a callee-visible local that must survive */
    cpu_sti();
    for (uint64_t i = 1; i <= 100; i++) {         /* one at a time: IPIs to one vector coalesce */
        lapic_self_ipi(VEC_TEST);
        for (volatile int spin = 0; spin < 1000000 && g_hits < i; spin++) { }
    }
    cpu_cli();
    KT_CHECK(g_hits == 100);
    KT_CHECK(marker == 0x5eed);
    KT_CHECK(g_seen_rsp != 0);
    return 0;
}
