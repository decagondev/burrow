/* irq.c - see irq.h. One lock for every line: the 8259 mask registers are
 * shared between lines, and the ISR takes the same lock (IF is already off
 * there, so a holder can never be interrupted by its own line). */
#include "irq.h"
#include "apic.h"
#include "trap.h"
#include "sched.h"
#include "spinlock.h"
#include "errno.h"
#include "kprintf.h"

#define IRQ_CASCADE 2u

struct line {
    struct thread *waiter;
    uint64_t count;
    int pending;            /* fired since the last irq_wait returned */
    int armed;              /* irq_wait has unmasked it at least once */
};

static struct spinlock g_lock = SPINLOCK_INIT("irq");
static struct line g_lines[IRQ_LINES];
static uint64_t g_spurious;

static int line_ok(uint64_t n) { return n < IRQ_LINES && n != IRQ_CASCADE; }

static void on_isa_irq(struct trap_frame *f) {
    unsigned n = (unsigned)(f->vector - VEC_IRQ_BASE);
    if ((n == 7 || n == 15) && !pic_in_service(n)) {      /* PIT-28: nothing is in service */
        if (n == 15) pic_eoi(IRQ_CASCADE);                 /* the master did see the cascade */
        g_spurious++;
        return;
    }
    uint64_t fl = spin_lock_irqsave(&g_lock);
    pic_set_masked(n, 1);                                 /* until the driver acks */
    pic_eoi(n);
    struct line *l = &g_lines[n];
    l->pending = 1;
    l->count++;
    if (l->waiter) { thread_wake(l->waiter); l->waiter = 0; }
    spin_unlock_irqrestore(&g_lock, fl);
}

void irq_init(void) {
    for (unsigned n = 0; n < IRQ_LINES; n++) trap_set_handler((uint8_t)(VEC_IRQ_BASE + n), on_isa_irq);
    uint64_t fl = spin_lock_irqsave(&g_lock);
    pic_set_masked(IRQ_CASCADE, 0);                       /* slave lines still masked on the slave */
    spin_unlock_irqrestore(&g_lock, fl);
    kprintf("[nucleus] irq ok (isa lines 0-15 on vectors %x-%x, 8259 via lint0)\n",
            VEC_IRQ_BASE, VEC_IRQ_BASE + IRQ_LINES - 1);
}

long irq_wait(uint64_t n) {
    if (!line_ok(n)) return -EINVAL;
    struct line *l = &g_lines[n];
    struct thread *me = thread_current();
    uint64_t fl = spin_lock_irqsave(&g_lock);
    if (l->waiter && l->waiter != me) { spin_unlock_irqrestore(&g_lock, fl); return -EBUSY; }
    if (!l->armed) { l->armed = 1; pic_set_masked(n, 0); }
    while (!l->pending) {
        l->waiter = me;
        thread_block_unlock(0, &g_lock);                  /* blocked before the lock drops (PIT-23) */
        spin_lock_irqsave(&g_lock);                       /* IF is already off: flags unused */
    }
    l->pending = 0;
    spin_unlock_irqrestore(&g_lock, fl);
    return 0;
}

long irq_ack(uint64_t n) {
    if (!line_ok(n)) return -EINVAL;
    uint64_t fl = spin_lock_irqsave(&g_lock);
    if (g_lines[n].armed) pic_set_masked(n, 0);
    spin_unlock_irqrestore(&g_lock, fl);
    return 0;
}

int irq_pending(uint64_t n)     { return n < IRQ_LINES && g_lines[n].pending; }
uint64_t irq_count(uint64_t n)  { return n < IRQ_LINES ? g_lines[n].count : 0; }
uint64_t irq_spurious(void)     { return g_spurious; }
