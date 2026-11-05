/* pat.c - see pat.h. Writes the PAT only at start-up, before any page uses
 * entry 1, so no cache or TLB holds a line of the old type (the SDM's
 * flush is then a TLB reload); never changes it later. Every CPU gets the
 * same value (PIT-65). */
#include "pat.h"
#include "paging.h"
#include "cpu.h"
#include "kprintf.h"

#define CPUID_EDX_PAT  (1u << 16)
#define PAT_ENTRY_BITS 8u
#define PAT_ENTRY_MASK 0xFFull

static uint64_t g_pat;          /* the value every CPU loads; 0 = no PAT */

int pat_has_wc(void) { return g_pat != 0; }

uint64_t pat_wc_flags(void) { return g_pat ? PTE_PWT : PTE_PCD | PTE_PWT; }

void pat_init_cpu(void) {
    if (!g_pat) return;
    wrmsr(MSR_PAT, g_pat);
    write_cr3(read_cr3());      /* drop any translation cached under the old table */
}

void pat_init(void) {
    uint32_t a, b, c, d;
    cpuid(1, &a, &b, &c, &d);
    if (!(d & CPUID_EDX_PAT)) {
        kprintf("[nucleus] pat: none (a write-combining request maps uncached)\n");
        return;
    }
    uint64_t shift = PAT_WC_INDEX * PAT_ENTRY_BITS;
    g_pat = (rdmsr(MSR_PAT) & ~(PAT_ENTRY_MASK << shift)) | ((uint64_t)PAT_TYPE_WC << shift);
    pat_init_cpu();
    kprintf("[nucleus] pat ok (wc at index %u, pat=%llx)\n", PAT_WC_INDEX, (unsigned long long)g_pat);
}
