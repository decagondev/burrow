/* regions.h - reserved address ranges of one address space. Pure logic.
 *
 * A reservation owns addresses, not memory: frames appear only when part
 * of it is committed (vm_region.c). The Go runtime reserves far more than
 * the machine's RAM up front (PIT-05), so this must never allocate frames.
 * Kept sorted by start; neighbours are never merged (simpler, and release
 * can split one in two). */
#ifndef NUCLEUS_REGIONS_H
#define NUCLEUS_REGIONS_H
#include <stdint.h>

#define REGIONS_MAX 64u

struct region { uint64_t start, len; };

struct regions {
    uint64_t lo, hi;          /* every reservation lies inside [lo, hi)          */
    uint64_t search_base;     /* hint-less reservations: first fit from here up  */
    uint32_t count;
    struct region r[REGIONS_MAX];
};

void regions_init(struct regions *rs, uint64_t lo, uint64_t hi, uint64_t search_base);
/* len and hint are page multiples. Uses hint if that range is free and inside
 * [lo, hi), else the first gap at or above search_base. 0 ok, -1 no room. */
int  regions_reserve(struct regions *rs, uint64_t hint, uint64_t len, uint64_t *out);
/* 1 if every byte of [addr, addr+len) is reserved. */
int  regions_covers(const struct regions *rs, uint64_t addr, uint64_t len);
/* Removes [addr, addr+len) from whatever reservations it overlaps (may split
 * one). 0 ok, -1 if a split would exceed REGIONS_MAX. */
int  regions_release(struct regions *rs, uint64_t addr, uint64_t len);
#endif
