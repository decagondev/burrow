/* apic.c - see apic.h. Intel SDM vol. 3, ch. 11; 8259A datasheet. */
#include "apic.h"
#include "trap.h"
#include "vmm.h"
#include "io.h"
#include "cpu.h"
#include "kprintf.h"
#include "panic.h"

#define PIC1_CMD 0x20
#define PIC1_DATA 0x21
#define PIC2_CMD 0xA0
#define PIC2_DATA 0xA1

#define MSR_APIC_BASE     0x1Bu
#define APIC_BASE_ENABLE  (1ull << 11)
#define SVR_ENABLE        (1u << 8)
#define ICR_SELF          (1u << 18)   /* destination shorthand 01 = self */
#define ICR_PENDING       (1u << 12)
#define LVT_EXTINT        (7u << 8)    /* delivery mode ExtINT: the 8259 supplies the vector */

static volatile uint32_t *g_lapic;

void pic_disable(void) {
    outb(PIC1_CMD, 0x11);  io_wait();      /* ICW1: init, ICW4 follows */
    outb(PIC2_CMD, 0x11);  io_wait();
    outb(PIC1_DATA, VEC_IRQ_BASE);     io_wait();   /* ICW2: vector offsets */
    outb(PIC2_DATA, VEC_IRQ_BASE + 8); io_wait();
    outb(PIC1_DATA, 4);    io_wait();      /* ICW3: slave on IRQ2 */
    outb(PIC2_DATA, 2);    io_wait();
    outb(PIC1_DATA, 1);    io_wait();      /* ICW4: 8086 mode */
    outb(PIC2_DATA, 1);    io_wait();
    outb(PIC1_DATA, 0xFF);                 /* mask everything */
    outb(PIC2_DATA, 0xFF);
}

/* IMR read-modify-write: callers hold IRQs off (irq.c's lock or an ISR). */
void pic_set_masked(unsigned line, int masked) {
    uint16_t port = line < 8 ? PIC1_DATA : PIC2_DATA;
    uint8_t bit = (uint8_t)(1u << (line & 7)), m = inb(port);
    outb(port, masked ? (uint8_t)(m | bit) : (uint8_t)(m & ~bit));
}

/* Non-specific EOI; a slave line needs one on both chips. */
void pic_eoi(unsigned line) {
    if (line >= 8) outb(PIC2_CMD, 0x20);
    outb(PIC1_CMD, 0x20);
}

/* OCW3 "read ISR". A spurious IRQ 7/15 arrives with its ISR bit clear. */
int pic_in_service(unsigned line) {
    uint16_t port = line < 8 ? PIC1_CMD : PIC2_CMD;
    outb(port, 0x0B);
    return (inb(port) >> (line & 7)) & 1;
}

uint32_t lapic_read(uint32_t reg)          { return g_lapic[reg / 4]; }
void     lapic_write(uint32_t reg, uint32_t v) { g_lapic[reg / 4] = v; }
void     lapic_eoi(void)                   { lapic_write(LAPIC_EOI, 0); }

void lapic_init(void) {
    uint32_t a, b, c, d;
    cpuid(1, &a, &b, &c, &d);
    if (!(d & (1u << 9))) panic("CPU has no local APIC");
    uint64_t base = rdmsr(MSR_APIC_BASE);
    wrmsr(MSR_APIC_BASE, base | APIC_BASE_ENABLE);
    g_lapic = vm_map_mmio(base & ~0xFFFull & 0x000FFFFFFFFFF000ull, 0x1000);
    lapic_write(LAPIC_TPR, 0);                              /* accept every priority */
    lapic_write(LAPIC_SVR, SVR_ENABLE | VEC_SPURIOUS);
    lapic_write(LAPIC_LVT_LINT0, LVT_EXTINT);              /* virtual wire: the 8259 drives LINT0 */
    kprintf("[nucleus] lapic ok (id=%u, version=%x, base=%llx), pic masked\n",
            lapic_read(LAPIC_ID) >> 24, lapic_read(LAPIC_VERSION) & 0xFF,
            (unsigned long long)(base & ~0xFFFull));
}

void lapic_self_ipi(uint8_t vector) {
    lapic_write(LAPIC_ICR_HI, 0);
    lapic_write(LAPIC_ICR_LO, ICR_SELF | vector);
    while (lapic_read(LAPIC_ICR_LO) & ICR_PENDING) { }
}
