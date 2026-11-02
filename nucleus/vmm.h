/* vmm.h - virtual memory MECHANISM (MEMORY_MAP.md §3): map, unmap, protect,
 * translate on any address space, through the physmap, with TLB flushes.
 * Policy (which ranges exist) belongs to vm_region.c and, later, Go.
 * Uses common/paging.c for the table walks. */
#ifndef NUCLEUS_VMM_H
#define NUCLEUS_VMM_H
#include <stdint.h>
#include "paging.h"
#include "regions.h"
#include "mutex.h"

struct addr_space {
    struct pt_ctx pt;          /* pt.root = PML4 physical address = CR3 value */
    struct regions regions;    /* reserved lower-half ranges (vm_region.h)    */
    struct kmutex lock;        /* serialises vm_region.c operations           */
};

/* Adopts the loader's tables as the kernel address space, drops the
 * temporary identity map (PIT-21) and turns on CR0.WP so ring 0 obeys
 * read-only pages too (W^X for the nucleus itself). */
void vmm_init(void);
struct addr_space *vmm_kernel(void);

#define PML4_KERNEL_FIRST 256   /* PML4 entries 256-511 = the upper half */

/* User address spaces (uspace.c): a new PML4 sharing the kernel half; and
 * freeing every lower-half table (not the frames they map) plus the PML4. */
int  vm_as_init_user(struct addr_space *as);   /* 0, or -1 out of memory */
void vm_as_free_tables(struct addr_space *as);

/* flags: PTE_W, PTE_U, PTE_PCD/PWT, PTE_G, PTE_NX (P implied). */
enum pt_status vm_map(struct addr_space *as, uint64_t va, uint64_t pa, uint64_t len, uint64_t flags);
/* free_frames: hand every unmapped frame back to frames.c. */
enum pt_status vm_unmap(struct addr_space *as, uint64_t va, uint64_t len, int free_frames);
enum pt_status vm_protect(struct addr_space *as, uint64_t va, uint64_t len, uint64_t flags);
enum pt_status vm_translate(const struct addr_space *as, uint64_t va, uint64_t *pa, uint64_t *flags);
/* Demand-zero pages (BUG-0012): discard frees a mapped page's frame and
 * leaves a marker; the #PF handler calls vm_fault_in, which maps a zeroed
 * frame at a marked page (1), reports a real fault (0) or out of memory
 * (-1: marked, no frame left). forget turns a
 * marker back into "absent", so PROT_NONE and released ranges fault. */
enum pt_status vm_discard_page(struct addr_space *as, uint64_t va);
enum pt_status vm_mark_lazy(struct addr_space *as, uint64_t va, uint64_t flags);   /* commit, no frame yet */
void vm_forget_discarded(struct addr_space *as, uint64_t va);
/* Present 4 KiB pages in the lower half (diagnostics: what Go has mapped). */
uint64_t vm_lower_half_pages(const struct addr_space *as);
int  vm_fault_in(struct addr_space *as, uint64_t va);
/* Maps device memory (uncached) at physmap_base + pa; returns its address. */
void *vm_map_mmio(uint64_t pa, uint64_t len);
#endif
