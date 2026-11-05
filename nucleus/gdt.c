/* gdt.c - see gdt.h. Descriptors are built at run time because the TSS
 * descriptor holds the TSS's (link-time) address. One GDT and TSS per CPU
 * (W20-S05, ADR-0011): RSP0 and the IST stacks differ per CPU. Intel SDM
 * vol. 3, §3.4.5, §7.2. */
#include "gdt.h"
#include "percpu.h"

struct tss {
    uint32_t reserved0;
    uint64_t rsp[3];       /* rsp[0] = RSP0 */
    uint64_t reserved1;
    uint64_t ist[7];       /* ist[0] = IST1 */
    uint64_t reserved2;
    uint16_t reserved3;
    uint16_t iopb_offset;  /* = sizeof: no I/O permission bitmap */
} __attribute__((packed));
_Static_assert(sizeof(struct tss) == 104, "TSS layout");

struct gdtr { uint16_t limit; uint64_t base; } __attribute__((packed));

/* Flat 64-bit descriptors: base 0, limit 4 GiB, G=1. */
#define DESC_KCODE 0x00AF9A000000FFFFull   /* P, DPL0, code exec/read, L=1 */
#define DESC_KDATA 0x00CF92000000FFFFull   /* P, DPL0, data read/write     */
#define DESC_UDATA 0x00CFF2000000FFFFull   /* P, DPL3, data read/write     */
#define DESC_UCODE 0x00AFFA000000FFFFull   /* P, DPL3, code exec/read, L=1 */

static uint64_t g_gdt[SMP_MAX_CPUS][8] __attribute__((aligned(16)));
static struct tss g_tss[SMP_MAX_CPUS] __attribute__((aligned(16)));
static uint8_t g_ist[3][IST_STACK_BYTES] __attribute__((aligned(16)));   /* the BSP's */

/* gdt_asm.asm: lgdt, far-return into SEL_KCODE, reload data segments, ltr. */
extern void gdt_load(const struct gdtr *gdtr, uint16_t kcode, uint16_t kdata, uint16_t tss);

static void set_tss_descriptor(uint64_t *slot, const struct tss *t) {
    uint64_t base = (uint64_t)(uintptr_t)t, limit = sizeof *t - 1;
    slot[0] = (limit & 0xFFFF) | ((base & 0xFFFFFF) << 16) | (0x89ull << 40) |   /* P, type 9 */
              (((limit >> 16) & 0xF) << 48) | (((base >> 24) & 0xFF) << 56);
    slot[1] = base >> 32;
}

void gdt_init_cpu(uint32_t cpu, const uint64_t ist_top[3]) {
    uint64_t *gdt = g_gdt[cpu];
    struct tss *tss = &g_tss[cpu];
    gdt[0] = 0;
    gdt[SEL_KCODE >> 3] = DESC_KCODE;
    gdt[SEL_KDATA >> 3] = DESC_KDATA;
    gdt[SEL_UCODE32 >> 3] = 0;
    gdt[SEL_UDATA >> 3] = DESC_UDATA;
    gdt[SEL_UCODE >> 3] = DESC_UCODE;
    set_tss_descriptor(&gdt[SEL_TSS >> 3], tss);

    tss->iopb_offset = sizeof *tss;
    for (unsigned i = 0; i < 3; i++) tss->ist[i] = ist_top[i];

    struct gdtr r = { sizeof g_gdt[0] - 1, (uint64_t)(uintptr_t)gdt };
    gdt_load(&r, SEL_KCODE, SEL_KDATA, SEL_TSS);
    percpu_bind(cpu);   /* gdt_load zeroed GS */
}

void gdt_init(void) {
    uint64_t top[3];
    for (unsigned i = 0; i < 3; i++) top[i] = (uint64_t)(uintptr_t)(g_ist[i] + IST_STACK_BYTES);
    gdt_init_cpu(0, top);
}

void tss_set_rsp0(uint64_t rsp0) { g_tss[this_cpu()->index].rsp[0] = rsp0; }
