/* irq.c - see irq.h. One lock for every line: the 8259's mask registers and
 * the I/O APIC's index register are shared between lines, and the ISRs take
 * the same lock (IF is already off there, so a holder can never be
 * interrupted by its own line). Every interrupt goes to the CPU that ran
 * irq_init (the BSP), as the 8259's always did.
 *
 * Masking: the 8259 latches an edge while a line is masked, so the old rule
 * "masked from the interrupt until the ack" loses nothing there; the I/O
 * APIC drops edges on a masked pin, so its edge lines stay unmasked once
 * armed and only its level lines are masked until the ack (W27-S01). */
#include "irq.h"
#include "irqroute.h"
#include "ioapic.h"
#include "acpi.h"
#include "apic.h"
#include "boot.h"
#include "io.h"
#include "trap.h"
#include "sched.h"
#include "spinlock.h"
#include "errno.h"
#include "kprintf.h"
#include "lib_string.h"

#define IRQ_CASCADE 2u
#define ALL_LINES   (IRQ_LINES + IRQ_MSI_LINES)
#define ELCR1       0x4D0
#define ELCR2       0x4D1

struct line {
    struct thread *waiter;
    uint64_t count;
    int pending;            /* fired since the last irq_wait returned */
    int armed;              /* irq_wait has unmasked it at least once */
    uint8_t kind;           /* IRQ_KIND_* */
    struct irq_route route; /* IRQ_KIND_IOAPIC */
};

static struct spinlock g_lock = SPINLOCK_INIT("irq");
static struct line g_lines[ALL_LINES];
static uint64_t g_spurious;
static uint8_t g_dest_apic;
static unsigned g_msi_used;

static int line_ok(uint64_t n) { return n < ALL_LINES && g_lines[n].kind != IRQ_KIND_NONE; }

/* Callers hold g_lock. */
static void set_masked(unsigned n, int masked) {
    struct line *l = &g_lines[n];
    if (l->kind == IRQ_KIND_PIC) pic_set_masked(n, masked);
    else if (l->kind == IRQ_KIND_IOAPIC) ioapic_set_masked(l->route.gsi, masked);
}

static void fired(struct line *l) {
    l->pending = 1;
    l->count++;
    if (l->waiter) { thread_wake(l->waiter); l->waiter = 0; }
}

static void on_isa_irq(struct trap_frame *f) {
    unsigned n = (unsigned)(f->vector - VEC_IRQ_BASE);
    struct line *l = &g_lines[n];
    if (l->kind == IRQ_KIND_PIC && (n == 7 || n == 15) && !pic_in_service(n)) {   /* PIT-28 */
        if (n == 15) pic_eoi(IRQ_CASCADE);                /* the master did see the cascade */
        g_spurious++;
        return;
    }
    uint64_t fl = spin_lock_irqsave(&g_lock);
    if (l->kind == IRQ_KIND_PIC) {
        pic_set_masked(n, 1);                             /* until the driver acks */
        pic_eoi(n);
    } else {
        if (l->route.level) ioapic_set_masked(l->route.gsi, 1);
        lapic_eoi();
    }
    fired(l);
    spin_unlock_irqrestore(&g_lock, fl);
}

static void on_msi(struct trap_frame *f) {
    uint64_t fl = spin_lock_irqsave(&g_lock);
    fired(&g_lines[IRQ_MSI_FIRST + (f->vector - VEC_MSI_BASE)]);
    lapic_eoi();
    spin_unlock_irqrestore(&g_lock, fl);
}

/* kargs "irq=pic": the 8259 even when there is an I/O APIC. */
static int kargs_say_pic(void) {
    const struct boot_module *m = boot_module_find("kargs");
    if (!m) return 0;
    const char *s = phys_to_virt(m->phys_start);
    for (uint64_t i = 0; i + 7 <= m->size_bytes; i++)
        if ((i == 0 || s[i - 1] == ' ' || s[i - 1] == '\n') && memcmp(s + i, "irq=pic", 7) == 0 &&
            (i + 7 == m->size_bytes || s[i + 7] == ' ' || s[i + 7] == '\n'))
            return 1;
    return 0;
}

/* Every ISA line on the I/O APIC, masked until irq_wait arms it; the 8259
 * stays masked and LINT0 too. 0 if there is no usable I/O APIC. */
static int route_through_ioapic(void) {
    const struct acpi_info *a = acpi_machine();
    if (!a || !a->ioapic_pa || kargs_say_pic()) return 0;
    uint32_t pins = ioapic_init(a->ioapic_pa, a->ioapic_gsi_base);
    if (!pins) return 0;
    uint16_t elcr = (uint16_t)(inb(ELCR1) | (inb(ELCR2) << 8));
    kprintf("[nucleus] irq: i/o apic %u at %llx, gsi %u-%u, elcr %x; routes", a->ioapic_id,
            (unsigned long long)a->ioapic_pa, a->ioapic_gsi_base, a->ioapic_gsi_base + pins - 1, elcr);
    for (unsigned n = 0; n < IRQ_LINES; n++) {
        if (n == IRQ_CASCADE) continue;
        struct line *l = &g_lines[n];
        irq_route_isa(a, n, elcr, &l->route);
        if (!ioapic_has(l->route.gsi)) continue;
        l->kind = IRQ_KIND_IOAPIC;
        ioapic_write_entry(l->route.gsi, ioapic_entry((uint8_t)(VEC_IRQ_BASE + n), g_dest_apic, &l->route, 1));
        if (l->route.gsi != n || l->route.level)
            kprintf(" %u:gsi%u%s%s", n, l->route.gsi, l->route.level ? ",level" : "",
                    l->route.active_low ? ",low" : "");
    }
    kprintf("\n");
    lapic_mask_lint0();
    return 1;
}

void irq_init(void) {
    for (unsigned n = 0; n < IRQ_LINES; n++) trap_set_handler((uint8_t)(VEC_IRQ_BASE + n), on_isa_irq);
    for (unsigned n = 0; n < IRQ_MSI_LINES; n++) trap_set_handler((uint8_t)(VEC_MSI_BASE + n), on_msi);
    g_dest_apic = (uint8_t)(lapic_read(LAPIC_ID) >> 24);
    uint64_t fl = spin_lock_irqsave(&g_lock);
    int ioapic = route_through_ioapic();
    if (!ioapic) {
        for (unsigned n = 0; n < IRQ_LINES; n++)
            if (n != IRQ_CASCADE) g_lines[n].kind = IRQ_KIND_PIC;
        pic_set_masked(IRQ_CASCADE, 0);                   /* slave lines still masked on the slave */
    }
    spin_unlock_irqrestore(&g_lock, fl);
    kprintf("[nucleus] irq ok (isa lines 0-15 on vectors %x-%x, %s; msi lines %u-%u on vectors %x-%x)\n",
            VEC_IRQ_BASE, VEC_IRQ_BASE + IRQ_LINES - 1, ioapic ? "i/o apic" : "8259 via lint0",
            IRQ_MSI_FIRST, ALL_LINES - 1, VEC_MSI_BASE, VEC_MSI_BASE + IRQ_MSI_LINES - 1);
}

long irq_wait(uint64_t n) {
    struct thread *me = thread_current();
    uint64_t fl = spin_lock_irqsave(&g_lock);
    if (!line_ok(n)) { spin_unlock_irqrestore(&g_lock, fl); return -EINVAL; }
    struct line *l = &g_lines[n];
    if (l->waiter && l->waiter != me) { spin_unlock_irqrestore(&g_lock, fl); return -EBUSY; }
    if (!l->armed) { l->armed = 1; set_masked((unsigned)n, 0); }
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
    uint64_t fl = spin_lock_irqsave(&g_lock);
    if (!line_ok(n)) { spin_unlock_irqrestore(&g_lock, fl); return -EINVAL; }
    if (g_lines[n].armed) set_masked((unsigned)n, 0);
    spin_unlock_irqrestore(&g_lock, fl);
    return 0;
}

long irq_alloc_msi(uint64_t *addr, uint32_t *data) {
    uint64_t fl = spin_lock_irqsave(&g_lock);
    if (g_msi_used == IRQ_MSI_LINES) { spin_unlock_irqrestore(&g_lock, fl); return -ENOSPC; }
    unsigned n = IRQ_MSI_FIRST + g_msi_used++;
    g_lines[n].kind = IRQ_KIND_MSI;
    g_lines[n].armed = 1;
    spin_unlock_irqrestore(&g_lock, fl);
    msi_message(g_dest_apic, (uint8_t)(VEC_MSI_BASE + n - IRQ_MSI_FIRST), addr, data);
    return n;
}

long irq_route(uint64_t n) {
    if (!line_ok(n)) return -EINVAL;
    const struct line *l = &g_lines[n];
    uint64_t where = l->kind == IRQ_KIND_IOAPIC ? l->route.gsi
                   : l->kind == IRQ_KIND_MSI ? VEC_MSI_BASE + n - IRQ_MSI_FIRST : n;
    uint64_t flags = (l->kind == IRQ_KIND_IOAPIC && l->route.level ? IRQ_ROUTE_LEVEL : 0) |
                     (l->kind == IRQ_KIND_IOAPIC && l->route.active_low ? IRQ_ROUTE_ACTIVE_LOW : 0);
    return (long)(l->kind | flags | (where << IRQ_ROUTE_SHIFT));
}

int irq_pending(uint64_t n)     { return n < ALL_LINES && g_lines[n].pending; }
uint64_t irq_count(uint64_t n)  { return n < ALL_LINES ? g_lines[n].count : 0; }
uint64_t irq_spurious(void)     { return g_spurious; }
int irq_uses_ioapic(void)       { return g_lines[0].kind == IRQ_KIND_IOAPIC; }
