/* vm_region.c - see vm_region.h. Interrupts are off for each call (one CPU). */
#include "vm_region.h"
#include "frames.h"
#include "errno.h"
#include "cpu.h"

static uint64_t round_up(uint64_t v) { return (v + PAGE_4K - 1) & ~(PAGE_4K - 1); }

static uint64_t prot_to_pte(int prot) {
    uint64_t f = 0;
    if (prot & VM_PROT_WRITE) f |= PTE_W;
    if (!(prot & VM_PROT_EXEC)) f |= PTE_NX;
    return f;                                  /* supervisor-only: the Go kernel is ring 0 */
}

void vm_region_init(struct addr_space *as) {
    regions_init(&as->regions, VM_LO, VM_HI, VM_SEARCH_BASE);
}

long vm_reserve(struct addr_space *as, uint64_t hint, uint64_t len, uint64_t *out) {
    if (len == 0 || (hint & (PAGE_4K - 1))) return -EINVAL;
    len = round_up(len);
    uint64_t f = irq_save();
    int rc = regions_reserve(&as->regions, hint, len, out);
    irq_restore(f);
    return rc ? -ENOMEM : 0;
}

/* One page: back it, re-protect it, or un-back it (PROT_NONE). */
static long commit_page(struct addr_space *as, uint64_t va, int prot) {
    uint64_t pa, fl;
    int mapped = vm_translate(as, va, &pa, &fl) == PT_OK;
    if (prot == VM_PROT_NONE) return mapped ? (vm_unmap(as, va, PAGE_4K, 1) == PT_OK ? 0 : -EINVAL) : 0;
    if (mapped) return vm_protect(as, va, PAGE_4K, prot_to_pte(prot)) == PT_OK ? 0 : -EINVAL;
    uint64_t frame = frame_alloc_zeroed();
    if (!frame) return -ENOMEM;
    if (vm_map(as, va, frame, PAGE_4K, prot_to_pte(prot)) != PT_OK) {
        frame_free(frame);
        return -ENOMEM;                        /* only failure left: no frame for a table */
    }
    return 0;
}

long vm_commit(struct addr_space *as, uint64_t addr, uint64_t len, int prot) {
    if (addr & (PAGE_4K - 1)) return -EINVAL;
    len = round_up(len);
    if (!regions_covers(&as->regions, addr, len)) return -EINVAL;
    for (uint64_t off = 0; off < len; off += PAGE_4K) {
        long rc = commit_page(as, addr + off, prot);
        if (rc) return rc;                     /* pages before stay committed */
    }
    return 0;
}

long vm_release(struct addr_space *as, uint64_t addr, uint64_t len) {
    if (addr & (PAGE_4K - 1)) return -EINVAL;
    len = round_up(len);
    if (!regions_covers(&as->regions, addr, len)) return -EINVAL;
    long rc = vm_commit(as, addr, len, VM_PROT_NONE);
    if (rc) return rc;
    uint64_t f = irq_save();
    int r = regions_release(&as->regions, addr, len);
    irq_restore(f);
    return r ? -ENOMEM : 0;
}
