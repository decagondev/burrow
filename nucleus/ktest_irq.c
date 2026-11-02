/* ktest_irq.c - interrupts with IF=1: the LAPIC delivers, the IRQ path
 * (IST1, copy to the kernel stack, dispatch, EOI, iretq) returns cleanly;
 * ISA lines wake the thread waiting in irq_wait (W7-S01). */
#include "ktest.h"
#include "apic.h"
#include "trap.h"
#include "cpu.h"
#include "time.h"
#include "kprintf.h"
#include "irq.h"
#include "io.h"
#include "sched.h"
#include "errno.h"

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
    uint64_t flags = read_rflags();               /* leave IF as we found it */
    cpu_sti();
    for (uint64_t i = 1; i <= 100; i++) {         /* one at a time: IPIs to one vector coalesce */
        lapic_self_ipi(VEC_TEST);
        for (volatile int spin = 0; spin < 1000000 && g_hits < i; spin++) { }
    }
    if (!(flags & RFLAGS_IF)) cpu_cli();
    KT_CHECK(g_hits == 100);
    KT_CHECK(marker == 0x5eed);
    KT_CHECK(g_seen_rsp != 0);
    return 0;
}

/* Two independent clocks must agree: a 100 ms TSC busy-wait sees 100 +- 5
 * LAPIC timer ticks, and monotonic time never goes backwards. */
int kt_time_tick_rate(void) {
    uint64_t last = time_monotonic_ns();
    uint64_t flags = read_rflags();
    cpu_sti();
    uint64_t t0 = time_monotonic_ns(), k0 = time_ticks();
    while (time_monotonic_ns() - t0 < 100ull * 1000 * 1000) {
        uint64_t now = time_monotonic_ns();
        KT_CHECK(now >= last);
        last = now;
    }
    uint64_t ticks = time_ticks() - k0;
    if (!(flags & RFLAGS_IF)) cpu_cli();
    kprintf("[ktest] time: %llu ticks in 100 ms (tsc %llu Hz)\n",
            (unsigned long long)ticks, (unsigned long long)time_tsc_hz());
    KT_CHECK(ticks >= 95 && ticks <= 105);
    return 0;
}

/* ---- ISA lines through the 8259 (W7-S01): PIT channel 0 in mode 0 raises
 * IRQ 0 once, when its count runs out. The BIOS left it running at 18.2 Hz,
 * and edges seen while the line was masked are latched by the 8259, so each
 * test first stops the PIT and drains whatever was latched. */
#define PIT_CH0      0x40
#define PIT_CMD      0x43
#define PIT_TICKS_MS 1193u
#define MS           1000000ull

static void pit_oneshot(uint16_t count) {
    outb(PIT_CMD, 0x30);                     /* channel 0, lo/hi byte, mode 0 */
    outb(PIT_CH0, (uint8_t)count);
    outb(PIT_CH0, (uint8_t)(count >> 8));
}

/* Afterwards: line 0 armed and unmasked, nothing pending, the PIT silent. */
static void irq0_quiesce(void) {
    pit_oneshot(0xFFFF);                     /* ~55 ms, then no more edges */
    irq_wait(0);                             /* a latched edge or that one */
    thread_sleep_ns(60 * MS);                /* the 55 ms edge is latched by now */
    irq_ack(0);
    thread_sleep_ns(1 * MS);
    if (irq_pending(0)) { irq_wait(0); irq_ack(0); }
}

int kt_irq_wait_pit(void) {
    irq0_quiesce();
    uint64_t n0 = irq_count(0), t0 = time_monotonic_ns();
    pit_oneshot(10 * PIT_TICKS_MS);
    KT_CHECK(irq_wait(0) == 0);
    uint64_t dt = time_monotonic_ns() - t0;
    irq_ack(0);
    kprintf("[ktest] irq: pit one-shot of 10 ms woke the waiter after %llu us\n",
            (unsigned long long)(dt / 1000));
    KT_CHECK(dt >= 9 * MS && dt <= 30 * MS);
    KT_CHECK(irq_count(0) == n0 + 1);
    return 0;
}

/* PIT-29: an interrupt with no waiter is kept, the next wait returns at once. */
int kt_irq_pending_before_wait(void) {
    irq0_quiesce();
    pit_oneshot(PIT_TICKS_MS);
    thread_sleep_ns(5 * MS);
    KT_CHECK(irq_pending(0));
    uint64_t t0 = time_monotonic_ns();
    KT_CHECK(irq_wait(0) == 0);
    KT_CHECK(time_monotonic_ns() - t0 < 1 * MS);
    KT_CHECK(!irq_pending(0));
    irq_ack(0);
    return 0;
}

/* The ISR masks the line; only irq_ack lets the next interrupt in. */
int kt_irq_masked_until_ack(void) {
    irq0_quiesce();
    pit_oneshot(PIT_TICKS_MS);
    KT_CHECK(irq_wait(0) == 0);              /* now masked, not acked */
    uint64_t n = irq_count(0);
    pit_oneshot(PIT_TICKS_MS);
    thread_sleep_ns(5 * MS);
    KT_CHECK(irq_count(0) == n && !irq_pending(0));
    irq_ack(0);                              /* the 8259 latched the edge: delivered now */
    thread_sleep_ns(1 * MS);
    KT_CHECK(irq_count(0) == n + 1 && irq_pending(0));
    KT_CHECK(irq_wait(0) == 0);
    irq_ack(0);
    return 0;
}

static long sys1(long nr, long a0) {
    long ret;
    __asm__ volatile("syscall" : "=a"(ret) : "a"(nr), "D"(a0) : "rcx", "r11", "memory");
    return ret;
}

/* The Go kernel's path: nu_irq_wait / nu_irq_ack through the SYSCALL gate. */
int kt_irq_syscall(void) {
    irq0_quiesce();
    pit_oneshot(2 * PIT_TICKS_MS);
    KT_CHECK(sys1(0x1000, 0) == 0);
    KT_CHECK(sys1(0x1001, 0) == 0);
    KT_CHECK(sys1(0x1000, 2) == -EINVAL);    /* the cascade */
    KT_CHECK(sys1(0x1000, 16) == -EINVAL);
    KT_CHECK(sys1(0x1001, 1ll << 32) == -EINVAL);
    KT_CHECK(irq_spurious() == 0);
    return 0;
}
