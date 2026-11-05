/* spinlock.c - the deadlock report, and kprintf's cross-CPU guard. The guard
 * never waits for ever: after KPRINT_SPINS it prints anyway, so a CPU that
 * panics inside a line (or while another CPU is stuck holding it) is heard.
 * spinlock.h; ADR-0011. */
#include "spinlock.h"
#include "panic.h"

#define KPRINT_SPINS (1u << 22)

static volatile uint32_t g_kprint_locked;

void spin_deadlock(const struct spinlock *l) {
    panic("spinlock '%s' never released (deadlock or re-entry)", l->name ? l->name : "?");
}

uint64_t kprint_enter(void) {
    uint64_t f = irq_save();
    for (uint32_t spins = 0; spins < KPRINT_SPINS; spins++) {
        if (!__atomic_exchange_n(&g_kprint_locked, 1, __ATOMIC_ACQUIRE)) return f;
        __asm__ volatile("pause");
    }
    return f;                           /* stuck holder: print over it */
}

void kprint_leave(uint64_t flags) {
    __atomic_store_n(&g_kprint_locked, 0, __ATOMIC_RELEASE);
    irq_restore(flags);
}
