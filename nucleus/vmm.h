/* vmm.h - virtual memory MECHANISM (MEMORY_MAP.md §3): map, unmap, protect,
 * translate on any address space, through the physmap, with TLB flushes.
 * Policy (which ranges exist) belongs to vm_region.c and, later, Go.
 * Uses common/paging.c for the table walks. */
#ifndef NUCLEUS_VMM_H
#define NUCLEUS_VMM_H
#include <stdint.h>
#include "paging.h"

struct addr_space {
    struct pt_ctx pt;          /* pt.root = PML4 physical address = CR3 value */
};

/* Adopts the loader's tables as the kernel address space, drops the
 * temporary identity map (PIT-21) and turns on CR0.WP so ring 0 obeys
 * read-only pages too (W^X for the nucleus itself). */
void vmm_init(void);
struct addr_space *vmm_kernel(void);

/* flags: PTE_W, PTE_U, PTE_PCD/PWT, PTE_G, PTE_NX (P implied). */
enum pt_status vm_map(struct addr_space *as, uint64_t va, uint64_t pa, uint64_t len, uint64_t flags);
/* free_frames: hand every unmapped frame back to frames.c. */
enum pt_status vm_unmap(struct addr_space *as, uint64_t va, uint64_t len, int free_frames);
enum pt_status vm_protect(struct addr_space *as, uint64_t va, uint64_t len, uint64_t flags);
enum pt_status vm_translate(const struct addr_space *as, uint64_t va, uint64_t *pa, uint64_t *flags);
/* Maps device memory (uncached) at physmap_base + pa; returns its address. */
void *vm_map_mmio(uint64_t pa, uint64_t len);
#endif
