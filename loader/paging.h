/* paging.h - x86-64 4-level page-table builder. Pure logic.
 *
 * Builds tables in memory it gets from the caller (Dependency Inversion):
 * `alloc_frame` hands out physical frames, `phys_to_ptr` turns a physical
 * address into a pointer the code can write through. In the loader these
 * are palloc + identity; in host tests they are a static pool with fake
 * physical addresses. This file never touches CR3 or any other CPU state.
 * See MEMORY_MAP.md §2 for which ranges get mapped. */
#ifndef LOADER_PAGING_H
#define LOADER_PAGING_H
#include <stdint.h>

#define PTE_P         (1ull << 0)    /* present                          */
#define PTE_W         (1ull << 1)    /* writable                         */
#define PTE_U         (1ull << 2)    /* user accessible                  */
#define PTE_PS        (1ull << 7)    /* 2 MiB page (in a PD entry)       */
#define PTE_G         (1ull << 8)    /* global                           */
#define PTE_NX        (1ull << 63)   /* no-execute: needs EFER.NXE=1     */
#define PTE_ADDR_MASK 0x000FFFFFFFFFF000ull

#define PAGE_4K 0x1000ull
#define PAGE_2M 0x200000ull

struct pt_ctx {
    uint64_t (*alloc_frame)(void *env);              /* 0 = out of memory */
    void    *(*phys_to_ptr)(void *env, uint64_t pa);
    void     *env;
    uint64_t  root;                                  /* physical address of the PML4 */
};

enum pt_status {
    PT_OK = 0,
    PT_NO_MEMORY,
    PT_UNALIGNED,         /* va, pa or size not 4 KiB aligned, or size 0 */
    PT_NON_CANONICAL,     /* any part of the range is in the 48-bit hole */
    PT_BAD_PHYS,          /* physical address beyond 52 bits             */
    PT_ALREADY_MAPPED,    /* refuses to silently replace a mapping       */
    PT_NOT_MAPPED,        /* pt_translate only                           */
};

/* Allocates and zeroes the PML4; sets c->root. */
enum pt_status pt_init(struct pt_ctx *c);
/* Maps [va, va+size) -> [pa, pa+size). P is implied; flags may add W, U, G, NX.
 * Uses 2 MiB pages wherever va and pa are 2 MiB aligned and enough remains.
 * On error, mappings made before the failing page stay in place. */
enum pt_status pt_map(struct pt_ctx *c, uint64_t va, uint64_t pa, uint64_t size, uint64_t flags);
/* Walks the tables like the MMU. *flags gets the leaf entry's non-address bits. */
enum pt_status pt_translate(const struct pt_ctx *c, uint64_t va, uint64_t *pa, uint64_t *flags);
const char *pt_status_str(enum pt_status s);
#endif
