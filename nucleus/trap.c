/* trap.c - IDT setup and trap_dispatch. See trap.h. */
#include "trap.h"
#include "gdt.h"
#include "percpu.h"
#include "kprintf.h"
#include "panic.h"
#include "cpu.h"
#include "syscall.h"
#include "uthread.h"
#include "vmm.h"

#define PF_PRESENT 1u   /* #PF error code bit 0: the page was present (a protection fault) */

struct idt_gate {
    uint16_t offset_lo, selector;
    uint8_t  ist, type_attr;       /* 0x8E: present, DPL 0, 64-bit interrupt gate */
    uint16_t offset_mid;
    uint32_t offset_hi, zero;
} __attribute__((packed));
_Static_assert(sizeof(struct idt_gate) == 16, "IDT gate");

struct idtr { uint16_t limit; uint64_t base; } __attribute__((packed));
struct fixup { uint64_t fault_rip, resume_rip; };

extern const uint64_t trap_stub_table[256];          /* trap.asm */
extern const struct fixup __fixup_start[], __fixup_end[];   /* linker.ld */
extern char boot_stack[], boot_stack_top[];          /* entry.asm */

struct percpu g_percpu;
volatile struct trap_record trap_last;
static struct idt_gate g_idt[256] __attribute__((aligned(16)));
static irq_handler g_handlers[256];

static const char *const exception_names[32] = {
    "#DE", "#DB", "NMI", "#BP", "#OF", "#BR", "#UD", "#NM", "#DF", "CSO", "#TS", "#NP",
    "#SS", "#GP", "#PF", "?15", "#MF", "#AC", "#MC", "#XM", "#VE", "#CP", "?22", "?23",
    "?24", "?25", "?26", "?27", "#HV", "#VC", "#SX", "?31",
};

const char *trap_name(uint64_t vector) {
    return vector < 32 ? exception_names[vector] : "irq";
}

static void set_gate(unsigned v, uint64_t handler, uint8_t ist) {
    struct idt_gate *g = &g_idt[v];
    g->offset_lo = (uint16_t)handler;
    g->selector = SEL_KCODE;
    g->ist = ist;
    g->type_attr = 0x8E;
    g->offset_mid = (uint16_t)(handler >> 16);
    g->offset_hi = (uint32_t)(handler >> 32);
    g->zero = 0;
}

void trap_init(void) {
    g_percpu.kstack_top = (uint64_t)(uintptr_t)boot_stack_top;
    g_percpu.kstack_bottom = (uint64_t)(uintptr_t)boot_stack;
    for (unsigned v = 0; v < 256; v++)
        set_gate(v, trap_stub_table[v], v == VEC_DF ? IST_DF : v == 2 ? IST_NMI : IST_IRQ);
    struct idtr r = { sizeof g_idt - 1, (uint64_t)(uintptr_t)g_idt };
    __asm__ volatile("lidt %0" :: "m"(r));
}

void trap_set_handler(uint8_t vector, irq_handler h) {
    if (vector < 32) panic("trap_set_handler: vector %u is an exception", vector);
    g_handlers[vector] = h;
}

static int try_fixup(struct trap_frame *f, uint64_t cr2) {
    for (const struct fixup *x = __fixup_start; x < __fixup_end; x++) {
        if (x->fault_rip != f->rip) continue;
        trap_last.vector = f->vector; trap_last.error = f->error;
        trap_last.cr2 = cr2;          trap_last.rip = f->rip;
        trap_last.count++;
        f->rip = x->resume_rip;
        return 1;
    }
    return 0;
}

/* Four qwords at a register's address, if readable (probe_read64 cannot fault
 * the report itself). Shows what a pointer register was pointing at. */
static void peek(const char *reg, uint64_t addr) {
    uint64_t v[4];
    addr &= ~7ull;
    for (int i = 0; i < 4; i++)
        if (probe_read64(addr + 8 * (uint64_t)i, &v[i]) != 0) return;
    kprintf("  [%s] %016llx: %016llx %016llx %016llx %016llx\n", reg, (unsigned long long)addr,
            (unsigned long long)v[0], (unsigned long long)v[1], (unsigned long long)v[2],
            (unsigned long long)v[3]);
}

static void __attribute__((noreturn)) report(const struct trap_frame *f, uint64_t cr2) {
    kprintf("[trap] %s at rip=%016llx err=%llx cr2=%016llx\n", trap_name(f->vector),
            (unsigned long long)f->rip, (unsigned long long)f->error, (unsigned long long)cr2);
    kprintf("  rax=%016llx rbx=%016llx rcx=%016llx rdx=%016llx\n",
            (unsigned long long)f->rax, (unsigned long long)f->rbx,
            (unsigned long long)f->rcx, (unsigned long long)f->rdx);
    kprintf("  rsi=%016llx rdi=%016llx rbp=%016llx rsp=%016llx\n",
            (unsigned long long)f->rsi, (unsigned long long)f->rdi,
            (unsigned long long)f->rbp, (unsigned long long)f->rsp);
    kprintf("  r8 =%016llx r9 =%016llx r10=%016llx r11=%016llx\n",
            (unsigned long long)f->r8, (unsigned long long)f->r9,
            (unsigned long long)f->r10, (unsigned long long)f->r11);
    kprintf("  r12=%016llx r13=%016llx r14=%016llx r15=%016llx\n",
            (unsigned long long)f->r12, (unsigned long long)f->r13,
            (unsigned long long)f->r14, (unsigned long long)f->r15);
    kprintf("  cs=%llx ss=%llx rflags=%llx\n", (unsigned long long)f->cs,
            (unsigned long long)f->ss, (unsigned long long)f->rflags);
    peek("rdx", f->rdx);
    peek("rsi", f->rsi);
    peek("rsp", f->rsp);
    if (f->rip >= go_text_lo && f->rip < go_text_hi) {      /* PIT-07: no SIGSEGV to turn into a Go panic */
        kprintf("[trap] fault in the Go kernel (%s, address %016llx): a nil or bad pointer in Go code\n",
                trap_name(f->vector), (unsigned long long)cr2);
        kprintf("[trap] find the line with: go tool addr2line build/kernel.elf <<< %llx\n",
                (unsigned long long)f->rip);
    }
    panic("unhandled %s (vector %llu)", trap_name(f->vector), (unsigned long long)f->vector);
}

void trap_dispatch(struct trap_frame *f);   /* called from trap.asm only */

void trap_dispatch(struct trap_frame *f) {
    int from_user = (f->cs & 3) == 3;
    if (f->vector < 32) {
        uint64_t cr2 = f->vector == VEC_PF ? read_cr2() : 0;
        /* A discarded page of kernel-space memory (the Go heap): zero-fill it (BUG-0012). */
        if (f->vector == VEC_PF && !from_user && !(f->error & PF_PRESENT)) {
            int r = vm_fault_in(vmm_kernel(), cr2);
            if (r > 0) return;
            if (r < 0) panic("out of memory: no frame for the page at %llx (rip %llx)",
                             (unsigned long long)cr2, (unsigned long long)f->rip);
        }
        if (from_user && f->vector != 2) { uthread_fault(f, cr2); return; }   /* Go decides; may resume (W10) */
        if (f->vector != VEC_DF && f->vector != 2 && try_fixup(f, cr2)) return;
        report(f, cr2);
    }
    if (f->vector == VEC_SPURIOUS) return;            /* LAPIC spurious: no EOI */
    irq_handler h = g_handlers[f->vector];
    if (!h) report(f, 0);
    h(f);
    if (from_user) uthread_return_to_user(f);       /* killed or asked to stop while in ring 3 */
}
