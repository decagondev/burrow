/* layout.h - addresses the loader chooses for Wave 4 (physical and virtual).
 * One definition each; MEMORY_MAP.md lists them. Change both together. */
#ifndef LOADER_LAYOUT_H
#define LOADER_LAYOUT_H

/* stage2 identity-maps the first 1 GiB; the loader can only touch RAM below it. */
#define LOADER_IDENTITY_LIMIT 0x40000000ull
/* palloc hands out frames from here up (everything below 1 MiB is firmware/boot data). */
#define PALLOC_FLOOR          0x100000ull

/* nucleus/linker.ld loads the nucleus at physical 1 MiB; it may use at most 3 MiB. */
#define NUCLEUS_PHYS_BASE     0x100000ull
#define NUCLEUS_PHYS_LIMIT    0x400000ull
/* -mcmodel=kernel: the nucleus image must live in the top 2 GiB. */
#define NUCLEUS_VIRT_BASE     0xFFFFFFFF80000000ull

/* All physical RAM, mapped linearly, supervisor-only, NX (MEMORY_MAP.md §2). */
#define PHYSMAP_BASE          0xFFFF800000000000ull
#endif
