/* vm_region.c - see vm_region.h. Every public call holds the address
 * space's mutex for its whole duration: the Go runtime's Ms mmap and
 * munmap concurrently, and a check-then-act on the region list or the page
 * tables must not interleave with another (BUG-0008). */
#include "vm_region.h"
#include "frames.h"
#include "boot.h"
#include "errno.h"
#include "lib_string.h"
#include "panic.h"

static uint64_t round_up(uint64_t v) { return (v + PAGE_4K - 1) & ~(PAGE_4K - 1); }

static uint64_t prot_to_pte(int prot) {
    uint64_t f = 0;
    if (prot & VM_PROT_WRITE) f |= PTE_W;
    if (!(prot & VM_PROT_EXEC)) f |= PTE_NX;
    return f;                                  /* supervisor-only: the Go kernel is ring 0 */
}

void vm_region_init(struct addr_space *as) {
    regions_init(&as->regions, VM_LO, VM_HI, VM_SEARCH_BASE);
    as->lock = (struct kmutex)KMUTEX_INIT;
}

/* ---- callers hold as->lock ------------------------------------------- */

static long reserve_locked(struct addr_space *as, uint64_t hint, uint64_t len, uint64_t *out) {
    return regions_reserve(&as->regions, hint, len, out) ? -ENOMEM : 0;
}

/* One page: back it, re-protect it, or un-back it (PROT_NONE). In a fresh
 * reservation nothing may be mapped yet: finding a page there means a
 * mapping leaked outside the region list, and reusing it would hand out
 * stale data as "new zeroed memory". */
static long commit_page(struct addr_space *as, uint64_t va, int prot, int fresh) {
    uint64_t pa, fl;
    int mapped = vm_translate(as, va, &pa, &fl) == PT_OK;
    if (mapped && fresh) panic("vm: page %llx already mapped in a fresh reservation", (unsigned long long)va);
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

static long commit_locked(struct addr_space *as, uint64_t addr, uint64_t len, int prot, int fresh) {
    if (!regions_covers(&as->regions, addr, len)) return -EINVAL;
    for (uint64_t off = 0; off < len; off += PAGE_4K) {
        long rc = commit_page(as, addr + off, prot, fresh);
        if (rc) return rc;                     /* pages before stay committed */
    }
    return 0;
}

static long release_locked(struct addr_space *as, uint64_t addr, uint64_t len) {
    long rc = commit_locked(as, addr, len, VM_PROT_NONE, 0);
    if (rc) return rc;
    return regions_release(&as->regions, addr, len) ? -ENOMEM : 0;
}

/* ---- public ------------------------------------------------------------ */

long vm_reserve(struct addr_space *as, uint64_t hint, uint64_t len, uint64_t *out) {
    if (len == 0 || (hint & (PAGE_4K - 1))) return -EINVAL;
    kmutex_lock(&as->lock);
    long rc = reserve_locked(as, hint, round_up(len), out);
    kmutex_unlock(&as->lock);
    return rc;
}

long vm_commit(struct addr_space *as, uint64_t addr, uint64_t len, int prot) {
    if (addr & (PAGE_4K - 1)) return -EINVAL;
    kmutex_lock(&as->lock);
    long rc = commit_locked(as, addr, round_up(len), prot, 0);
    kmutex_unlock(&as->lock);
    return rc;
}

long vm_release(struct addr_space *as, uint64_t addr, uint64_t len) {
    if (addr & (PAGE_4K - 1)) return -EINVAL;
    kmutex_lock(&as->lock);
    long rc = release_locked(as, addr, round_up(len));
    kmutex_unlock(&as->lock);
    return rc;
}

long vm_discard(struct addr_space *as, uint64_t addr, uint64_t len) {
    if (addr & (PAGE_4K - 1)) return -EINVAL;
    len = round_up(len);
    kmutex_lock(&as->lock);
    for (uint64_t off = 0; off < len; off += PAGE_4K) {
        uint64_t pa, fl;
        if (vm_translate(as, addr + off, &pa, &fl) == PT_OK)
            memset(phys_to_virt(pa & ~(PAGE_4K - 1)), 0, PAGE_4K);
    }
    kmutex_unlock(&as->lock);
    return 0;
}

long vm_map_anon(struct addr_space *as, uint64_t addr, uint64_t len, int prot, int flags, uint64_t *out) {
    if (len == 0 || (addr & (PAGE_4K - 1))) return -EINVAL;
    len = round_up(len);
    long rc;
    kmutex_lock(&as->lock);
    if (flags & (VM_MAP_FIXED | VM_MAP_NOREPLACE)) {
        if (regions_covers(&as->regions, addr, len)) {
            rc = flags & VM_MAP_NOREPLACE ? -EEXIST : 0;
        } else {
            uint64_t got;
            rc = reserve_locked(as, addr, len, &got);
            if (rc == 0 && got != addr) { release_locked(as, got, len); rc = -ENOMEM; }
        }
        /* A new anonymous mapping reads as zeros: drop old pages, then back. */
        if (rc == 0) rc = commit_locked(as, addr, len, VM_PROT_NONE, 0);
        if (rc == 0 && prot != VM_PROT_NONE) rc = commit_locked(as, addr, len, prot, 1);
        *out = addr;
    } else {
        rc = reserve_locked(as, addr, len, out);           /* honours a free hint */
        if (rc == 0 && prot != VM_PROT_NONE && (rc = commit_locked(as, *out, len, prot, 1)) != 0)
            release_locked(as, *out, len);
    }
    kmutex_unlock(&as->lock);
    return rc;
}
