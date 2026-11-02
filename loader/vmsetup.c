#include "vmsetup.h"
#include "layout.h"
#include "memmap.h"

#define PF_X 1u
#define PF_W 2u

uint64_t vm_physmap_size(const struct e820_entry *map, uint32_t count) {
    uint64_t top = e820_max_usable_addr(map, count);
    return (top + PAGE_2M - 1) & ~(PAGE_2M - 1);
}

static enum pt_status map_nucleus(struct pt_ctx *c, const struct vm_plan *p) {
    for (uint32_t i = 0; i < p->nucleus->count; i++) {
        const struct elf64_segment *s = &p->nucleus->seg[i];
        uint64_t va = s->vaddr & ~(PAGE_4K - 1), pa = s->paddr & ~(PAGE_4K - 1);
        uint64_t size = (s->vaddr + s->memsz - va + PAGE_4K - 1) & ~(PAGE_4K - 1);
        uint64_t flags = 0;
        if (s->flags & PF_W) flags |= PTE_W;
        if (!(s->flags & PF_X) && p->use_nx) flags |= PTE_NX;
        /* 4 KiB granularity is what makes W^X per segment possible; two
         * segments sharing a page fail here with PT_ALREADY_MAPPED. */
        enum pt_status st = pt_map(c, va, pa, size, flags);
        if (st != PT_OK) return st;
    }
    return PT_OK;
}

enum pt_status vm_build(struct pt_ctx *c, const struct vm_plan *p, const char **what) {
    enum pt_status st;
    *what = "identity map";
    if ((st = pt_map(c, 0, 0, LOADER_IDENTITY_LIMIT, PTE_W)) != PT_OK) return st;
    *what = "physmap";
    uint64_t size = vm_physmap_size(p->map, p->count);
    if ((st = pt_map(c, PHYSMAP_BASE, 0, size, PTE_W | (p->use_nx ? PTE_NX : 0))) != PT_OK) return st;
    *what = "nucleus";
    return map_nucleus(c, p);
}
