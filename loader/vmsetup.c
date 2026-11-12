#include "vmsetup.h"
#include "layout.h"
#include "memmap.h"

#define PF_X 1u
#define PF_W 2u

#define FOUR_GIB 0x100000000ull

/* RAM below 4 GiB is mapped from 0 to its top (rounded up to 2 MiB), so
 * firmware tables next to it stay reachable; above 4 GiB only the usable
 * ranges are, so the PCI hole below 4 GiB (device memory, mapped uncached by
 * vm_map_mmio) is never mapped as RAM (W14-S01). */
static uint64_t low_top(const struct e820_entry *map, uint32_t count) {
    uint64_t top = 0;
    for (uint32_t i = 0; i < count; i++) {
        uint64_t end = map[i].base + map[i].length;
        if (map[i].type == E820_USABLE && map[i].base < FOUR_GIB && end > top)
            top = end < FOUR_GIB ? end : FOUR_GIB;
    }
    return (top + PAGE_2M - 1) & ~(PAGE_2M - 1);
}

/* The part of entry i above 4 GiB, whole pages only (rounded inward, so
 * neighbouring entries never share a page): 0 if there is none. */
static uint64_t high_range(const struct e820_entry *e, uint64_t *base) {
    uint64_t lo = e->base > FOUR_GIB ? e->base : FOUR_GIB, end = e->base + e->length;
    lo = (lo + PAGE_4K - 1) & ~(PAGE_4K - 1);
    end &= ~(PAGE_4K - 1);
    *base = lo;
    return e->type == E820_USABLE && end > lo ? end - lo : 0;
}

uint64_t vm_physmap_size(const struct e820_entry *map, uint32_t count) {
    uint64_t size = low_top(map, count), base;
    for (uint32_t i = 0; i < count; i++) size += high_range(&map[i], &base);
    return size;
}

static enum pt_status map_physmap(struct pt_ctx *c, const struct vm_plan *p) {
    uint64_t flags = PTE_W | (p->use_nx ? PTE_NX : 0), base, len;
    enum pt_status st = pt_map(c, PHYSMAP_BASE, 0, low_top(p->map, p->count), flags);
    for (uint32_t i = 0; st == PT_OK && i < p->count; i++)
        if ((len = high_range(&p->map[i], &base)) != 0) st = pt_map(c, PHYSMAP_BASE + base, base, len, flags);
    return st;
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

/* The part of the loader above the identity map, whole 4 KiB pages, RWX. */
static enum pt_status map_loader(struct pt_ctx *c, const struct vm_plan *p) {
    if (!p->loader_bytes) return PT_OK;
    uint64_t lo = p->loader_base > LOADER_IDENTITY_LIMIT ? p->loader_base : LOADER_IDENTITY_LIMIT;
    uint64_t end = (p->loader_base + p->loader_bytes + PAGE_4K - 1) & ~(PAGE_4K - 1);
    lo &= ~(PAGE_4K - 1);
    return end > lo ? pt_map(c, lo, lo, end - lo, PTE_W) : PT_OK;
}

enum pt_status vm_build(struct pt_ctx *c, const struct vm_plan *p, const char **what) {
    enum pt_status st;
    *what = "identity map";
    if ((st = pt_map(c, 0, 0, LOADER_IDENTITY_LIMIT, PTE_W)) != PT_OK) return st;
    *what = "loader identity map";
    if ((st = map_loader(c, p)) != PT_OK) return st;
    *what = "physmap";
    if ((st = map_physmap(c, p)) != PT_OK) return st;
    *what = "nucleus";
    return map_nucleus(c, p);
}
