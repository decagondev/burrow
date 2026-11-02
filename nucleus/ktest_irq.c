/* ktest_irq.c - interrupts with IF=1: the LAPIC delivers, the IRQ path
 * (IST1, copy to the kernel stack, dispatch, EOI, iretq) returns cleanly. */
#include "ktest.h"
#include "apic.h"
#include "trap.h"
#include "cpu.h"
#include "time.h"
#include "kprintf.h"

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

/* Two independent clocks must agree: a 100 ms TSC busy-wait sees 100 +- 5
 * LAPIC timer ticks, and monotonic time never goes backwards. */
int kt_time_tick_rate(void) {
    uint64_t last = time_monotonic_ns();
    cpu_sti();
    uint64_t t0 = time_monotonic_ns(), k0 = time_ticks();
    while (time_monotonic_ns() - t0 < 100ull * 1000 * 1000) {
        uint64_t now = time_monotonic_ns();
        KT_CHECK(now >= last);
        last = now;
    }
    uint64_t ticks = time_ticks() - k0;
    cpu_cli();
    kprintf("[ktest] time: %llu ticks in 100 ms (tsc %llu Hz)\n",
            (unsigned long long)ticks, (unsigned long long)time_tsc_hz());
    KT_CHECK(ticks >= 95 && ticks <= 105);
    return 0;
}
