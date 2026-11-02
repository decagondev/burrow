/* gdt.c - see gdt.h. Descriptors are built at run time because the TSS
 * descriptor holds the TSS's (link-time) address. Intel SDM vol. 3, §3.4.5, §7.2. */
#include "gdt.h"

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

static uint64_t g_gdt[8] __attribute__((aligned(16)));
static struct tss g_tss __attribute__((aligned(16)));
static uint8_t g_ist[3][IST_STACK_BYTES] __attribute__((aligned(16)));

/* gdt_asm.asm: lgdt, far-return into SEL_KCODE, reload data segments, ltr. */
extern void gdt_load(const struct gdtr *gdtr, uint16_t kcode, uint16_t kdata, uint16_t tss);

static void set_tss_descriptor(uint64_t *slot, const struct tss *t) {
    uint64_t base = (uint64_t)(uintptr_t)t, limit = sizeof *t - 1;
    slot[0] = (limit & 0xFFFF) | ((base & 0xFFFFFF) << 16) | (0x89ull << 40) |   /* P, type 9 */
              (((limit >> 16) & 0xF) << 48) | (((base >> 24) & 0xFF) << 56);
    slot[1] = base >> 32;
}

void gdt_init(void) {
    g_gdt[0] = 0;
    g_gdt[SEL_KCODE >> 3] = DESC_KCODE;
    g_gdt[SEL_KDATA >> 3] = DESC_KDATA;
    g_gdt[SEL_UCODE32 >> 3] = 0;
    g_gdt[SEL_UDATA >> 3] = DESC_UDATA;
    g_gdt[SEL_UCODE >> 3] = DESC_UCODE;
    set_tss_descriptor(&g_gdt[SEL_TSS >> 3], &g_tss);

    g_tss.iopb_offset = sizeof g_tss;
    for (unsigned i = 0; i < 3; i++)
        g_tss.ist[i] = (uint64_t)(uintptr_t)(g_ist[i] + IST_STACK_BYTES);

    struct gdtr r = { sizeof g_gdt - 1, (uint64_t)(uintptr_t)g_gdt };
    gdt_load(&r, SEL_KCODE, SEL_KDATA, SEL_TSS);
}

void tss_set_rsp0(uint64_t rsp0) { g_tss.rsp[0] = rsp0; }
