/* protect.c - the CPU's protections of the kernel from user memory
 * (W20-S02): SMEP (the kernel may not execute a user page) and SMAP (the
 * kernel may not read or write one outside stac/clac) are switched on when
 * CPUID says the CPU has them, and NX (no execution of pages marked so,
 * set up by stage2 and the loader) is checked to be on. Burrow's kernels
 * reach user memory only through the physmap (kernel/proc's copy-in and
 * copy-out, uspace_translate), so no access needs stac. Never turns a
 * protection off. Docs: docs/04-plan/waves/WAVE-20-smp.md (S02). */
#include "protect.h"
#include "cpu.h"
#include "kprintf.h"

#define CPUID7_EBX_SMEP (1u << 7)
#define CPUID7_EBX_SMAP (1u << 20)
#define MSR_EFER        0xC0000080u
#define EFER_NXE        (1ull << 11)

static int g_smep, g_smap;

int protect_smep(void) { return g_smep; }
int protect_smap(void) { return g_smap; }

void protect_init(void) {
    uint32_t a, b, c, d;
    cpuid(0, &a, &b, &c, &d);
    if (a >= 7) {
        cpuid(7, &a, &b, &c, &d);
        g_smep = (b & CPUID7_EBX_SMEP) != 0;
        g_smap = (b & CPUID7_EBX_SMAP) != 0;
    }
    uint64_t cr4 = read_cr4();
    if (g_smep) cr4 |= CR4_SMEP;
    if (g_smap) cr4 |= CR4_SMAP;
    write_cr4(cr4);
    int nx = (rdmsr(MSR_EFER) & EFER_NXE) != 0;
    kprintf("[nucleus] protections: smep %s, smap %s, nx %s ok\n", g_smep ? "on" : "absent",
            g_smap ? "on" : "absent", nx ? "on" : "off");
}
