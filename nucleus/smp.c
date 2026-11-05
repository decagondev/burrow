/* smp.c - see smp.h. The BSP copies the trampoline (ap.asm) to
 * TRAMPOLINE_PA, a page stage2 used and nothing uses after boot, builds a
 * temporary PML4 below 4 GiB (the kernel's entries plus an identity map of
 * the first 2 MiB, which the trampoline runs in while paging comes on),
 * maps each AP an idle stack and three IST stacks, and starts the APs one
 * at a time with the SDM's INIT-SIPI-SIPI sequence and delays. An AP
 * switches to the kernel CR3, loads its own GDT/TSS (binding GS), the IDT,
 * its local APIC, FPU and SYSCALL MSRs, becomes its own idle thread, starts
 * its timer, checks in, and schedules (W20-S06). Never prints from an AP
 * (the BSP reports for it). Intel SDM vol. 3, §8.4.4; ADR-0011. */
#include "smp.h"
#include "percpu.h"
#include "acpi.h"
#include "apic.h"
#include "gdt.h"
#include "trap.h"
#include "time.h"
#include "frames.h"
#include "vmm.h"
#include "boot.h"
#include "fpu.h"
#include "syscall.h"
#include "sched.h"
#include "ktest.h"
#include "cpu.h"
#include "kprintf.h"
#include "lib_string.h"

#define TRAMPOLINE_PA    0x8000ull        /* ap.asm's TRAMPOLINE_PA */
#define SIPI_VECTOR      (TRAMPOLINE_PA >> 12)
#define ICR_INIT         (5u << 8)
#define ICR_STARTUP      (6u << 8)
#define ICR_LEVEL_ASSERT (1u << 14)
#define INIT_WAIT_NS     10000000ull      /* SDM: 10 ms after INIT */
#define SIPI_WAIT_NS     200000ull        /* 200 us after each STARTUP */
#define CHECKIN_WAIT_NS  1000000000ull    /* TCG can be slow to start a vCPU */
#define FOUR_GIB         0x100000000ull
#define EFER_LMA         (1ull << 10)     /* read-only: never written */
#define CR4_PCIDE        (1ull << 17)     /* needs long mode already: never set by the trampoline */
#define OP_JMP_SELF      0xFEEBu          /* "jmp $": REG-0008 writes it over the trampoline (a hlt resumes on the next SIPI) */
/* AP stacks: per CPU an idle stack and three IST stacks, each above a guard page. */
#define AP_STACK_REGION  0xFFFFFFFFC0200000ull
#define AP_STACK_BYTES   16384ull
#define AP_STACK_SLOT    (AP_STACK_BYTES + PAGE_4K)
#define AP_STACKS        4u

struct ap_boot { uint64_t cr3, cr4, efer, cr0, stack, entry, arg; };   /* ap.asm's data block */
extern char ap_tramp_start[], ap_tramp_end[], ap_tramp_data[];
extern uint64_t g_kernel_cr3;   /* shim.c */

struct percpu g_cpus[SMP_MAX_CPUS];
static uint32_t g_online = 1;
static uint64_t g_ap_stack_top[SMP_MAX_CPUS][AP_STACKS];   /* [0] idle, [1..3] IST1..3 */

void ap_main(uint64_t index) __attribute__((noreturn));   /* the trampoline calls it */

uint32_t smp_cpu_count(void) { return g_online; }

static void on_wake(struct trap_frame *f) { (void)f; lapic_eoi(); }

/* Maps the AP's four stacks; 0 ok. */
static int map_ap_stacks(uint32_t index) {
    uint64_t cpu_base = AP_STACK_REGION + (uint64_t)index * AP_STACKS * AP_STACK_SLOT;
    for (uint32_t k = 0; k < AP_STACKS; k++) {
        uint64_t base = cpu_base + k * AP_STACK_SLOT + PAGE_4K;   /* guard below */
        for (uint64_t off = 0; off < AP_STACK_BYTES; off += PAGE_4K) {
            uint64_t pa = frame_alloc_zeroed();
            if (!pa) return -1;
            if (vm_map(vmm_kernel(), base + off, pa, PAGE_4K, PTE_W | PTE_NX) != PT_OK) {
                frame_free(pa);
                return -1;
            }
        }
        g_ap_stack_top[index][k] = base + AP_STACK_BYTES;
    }
    return 0;
}

/* The temporary PML4 (and the PDPT and PD of its identity map); 0 ok. */
static int build_boot_tables(uint64_t pa[3]) {
    for (int i = 0; i < 3; i++) {
        pa[i] = frame_alloc_zeroed();
        if (!pa[i] || pa[i] >= FOUR_GIB) return -1;   /* the trampoline loads CR3 in real mode */
    }
    uint64_t *pml4 = phys_to_virt(pa[0]), *pdpt = phys_to_virt(pa[1]), *pd = phys_to_virt(pa[2]);
    memcpy(pml4, phys_to_virt(g_kernel_cr3 & PTE_ADDR_MASK), PAGE_4K);
    pml4[0] = pa[1] | PTE_P | PTE_W;
    pdpt[0] = pa[2] | PTE_P | PTE_W;
    pd[0] = 0 | PTE_P | PTE_W | PTE_PS;   /* 0-2 MiB, the trampoline page in it */
    return 0;
}

static int start_ap(uint32_t index, uint8_t apic_id, struct ap_boot *b) {
    b->stack = g_ap_stack_top[index][0];
    b->arg = index;
    g_cpus[index].apic_id = apic_id;
    __atomic_thread_fence(__ATOMIC_SEQ_CST);
    lapic_send_ipi(apic_id, ICR_INIT | ICR_LEVEL_ASSERT);
    time_busy_wait_ns(INIT_WAIT_NS);
    for (int i = 0; i < 2 && !__atomic_load_n(&g_cpus[index].online, __ATOMIC_ACQUIRE); i++) {
        lapic_send_ipi(apic_id, ICR_STARTUP | (uint32_t)SIPI_VECTOR);
        time_busy_wait_ns(SIPI_WAIT_NS);
    }
    uint64_t until = time_monotonic_ns() + CHECKIN_WAIT_NS;
    while (!__atomic_load_n(&g_cpus[index].online, __ATOMIC_ACQUIRE))
        if (time_monotonic_ns() > until) return -1;
    return 0;
}

static void start_all(const struct acpi_info *m, struct ap_boot *b) {
    uint32_t self = lapic_read(LAPIC_ID) >> 24;
    g_cpus[0].apic_id = self;
    g_cpus[0].online = 1;
    for (uint32_t i = 0; i < m->ncpus; i++) {
        if (m->apic_id[i] == self) continue;
        uint32_t index = g_online;
        if (index >= SMP_MAX_CPUS) {
            kprintf("[nucleus] smp: more than %u CPUs; the rest stay off\n", SMP_MAX_CPUS);
            return;
        }
        if (map_ap_stacks(index) != 0) {
            kprintf("[nucleus] smp: no memory for cpu %u's stacks\n", index);
            return;
        }
        if (start_ap(index, m->apic_id[i], b) != 0) {
            kprintf("[nucleus] smp: cpu with apic id %u did not start\n", m->apic_id[i]);
            continue;
        }
        g_online++;
        kprintf("[nucleus] smp: cpu %u online (apic id %u)\n", index, m->apic_id[i]);
    }
}

void smp_init(void) {
    const struct acpi_info *m = acpi_machine();
    trap_set_handler(VEC_WAKE, on_wake);
    uint64_t tables[3] = {0, 0, 0};
    uint64_t tramp_bytes = (uint64_t)(ap_tramp_end - ap_tramp_start);
    if (m && m->ncpus > 1 && tramp_bytes <= PAGE_4K && build_boot_tables(tables) == 0) {
        memcpy(phys_to_virt(TRAMPOLINE_PA), ap_tramp_start, tramp_bytes);
        if (ktest_listed("smp_broken_trampoline")) *(uint16_t *)phys_to_virt(TRAMPOLINE_PA) = OP_JMP_SELF;
        struct ap_boot *b = phys_to_virt(TRAMPOLINE_PA + (uint64_t)(ap_tramp_data - ap_tramp_start));
        b->cr3 = tables[0];
        b->cr4 = read_cr4() & ~CR4_PCIDE;
        b->efer = rdmsr(MSR_EFER) & ~EFER_LMA;
        b->cr0 = read_cr0();
        b->entry = (uint64_t)(uintptr_t)ap_main;
        start_all(m, b);
    } else {
        g_cpus[0].online = 1;
    }
    for (int i = 0; i < 3; i++)
        if (tables[i]) frame_free(tables[i]);
    kprintf("[nucleus] smp: %u CPU%s online\n", g_online, g_online == 1 ? "" : "s");
}

void smp_mailbox_run(void) {
    struct percpu *c = this_cpu();
    void (*fn)(void *) = __atomic_load_n(&c->fn, __ATOMIC_ACQUIRE);
    if (!fn) return;
    c->fn = 0;
    cpu_sti();
    fn(c->arg);
    cpu_cli();
    __atomic_fetch_add(&c->done, 1, __ATOMIC_RELEASE);
}

void ap_main(uint64_t index) {
    write_cr3(g_kernel_cr3);                  /* off the temporary tables at once */
    gdt_init_cpu((uint32_t)index, &g_ap_stack_top[index][1]);
    struct percpu *c = this_cpu();
    c->kstack_top = g_ap_stack_top[index][0];
    c->kstack_bottom = c->kstack_top - AP_STACK_BYTES;
    trap_init_cpu();
    lapic_init_cpu();
    fpu_init_cpu();
    syscall_cpu_init();
    sched_init_cpu(c->kstack_bottom, c->kstack_top);
    time_init_cpu();
    __atomic_store_n(&c->online, 1, __ATOMIC_RELEASE);
    sched_idle();
}

void smp_run_everywhere(void (*fn)(void *), void *arg) {
    uint64_t before[SMP_MAX_CPUS];
    for (uint32_t i = 1; i < g_online; i++) {
        struct percpu *c = &g_cpus[i];
        before[i] = __atomic_load_n(&c->done, __ATOMIC_ACQUIRE);
        c->arg = arg;
        __atomic_store_n(&c->fn, fn, __ATOMIC_RELEASE);
        lapic_send_ipi((uint8_t)c->apic_id, VEC_WAKE);
    }
    fn(arg);
    for (uint32_t i = 1; i < g_online; i++)
        while (__atomic_load_n(&g_cpus[i].done, __ATOMIC_ACQUIRE) == before[i]) cpu_pause();
}
