#include "palloc.h"

static uint64_t align_up(uint64_t v) { return (v + PAGE_SIZE - 1) & ~(uint64_t)(PAGE_SIZE - 1); }

static int overlaps(uint64_t a, uint64_t alen, uint64_t b, uint64_t blen) {
    return a < b + blen && b < a + alen;
}

void palloc_init(struct palloc *p, const struct e820_entry *map, uint32_t count,
                 uint64_t floor, uint64_t limit) {
    p->map = map; p->count = count; p->nreserved = 0;
    p->floor = floor; p->limit = limit;
    p->next = floor; p->first = 0; p->end = 0;
}

int palloc_reserve(struct palloc *p, uint64_t base, uint64_t size_bytes) {
    if (p->nreserved == PALLOC_MAX_RESERVED) return -1;
    p->reserved[p->nreserved].base = base;
    p->reserved[p->nreserved].size_bytes = size_bytes;
    p->nreserved++;
    return 0;
}

/* Lowest usable entry base above `addr`, or 0 if there is none. */
static uint64_t next_usable_base(const struct palloc *p, uint64_t addr) {
    uint64_t best = 0;
    for (uint32_t i = 0; i < p->count; i++) {
        const struct e820_entry *e = &p->map[i];
        if (e->type == E820_USABLE && e->base > addr && (best == 0 || e->base < best))
            best = e->base;
    }
    return best;
}

/* 0 if [cand, cand+len) is allocatable, else a strictly larger address to try next
 * (or UINT64_MAX when nothing further can work). */
static uint64_t conflict(const struct palloc *p, uint64_t cand, uint64_t len) {
    const struct e820_entry *home = 0;
    for (uint32_t i = 0; i < p->count; i++) {
        const struct e820_entry *e = &p->map[i];
        if (e->type == E820_USABLE && e->base <= cand && cand < e->base + e->length) home = e;
    }
    if (!home || cand + len > home->base + home->length) {
        uint64_t nb = next_usable_base(p, cand);
        return nb ? nb : UINT64_MAX;
    }
    for (uint32_t i = 0; i < p->count; i++) {      /* E820 entries may overlap */
        const struct e820_entry *e = &p->map[i];
        if (e->type != E820_USABLE && overlaps(cand, len, e->base, e->length))
            return e->base + e->length;
    }
    for (uint32_t i = 0; i < p->nreserved; i++) {
        const struct phys_range *r = &p->reserved[i];
        if (overlaps(cand, len, r->base, r->size_bytes)) return r->base + r->size_bytes;
    }
    return 0;
}

uint64_t palloc_alloc(struct palloc *p, uint64_t count_frames) {
    if (count_frames == 0 || count_frames > p->limit / PAGE_SIZE) return 0;
    uint64_t len = count_frames * PAGE_SIZE;
    uint64_t cand = p->next;
    for (;;) {
        if (cand < p->floor) cand = p->floor;
        cand = align_up(cand);
        if (cand == 0 || cand > p->limit || len > p->limit - cand) return 0;
        uint64_t retry = conflict(p, cand, len);
        if (retry == 0) break;
        if (retry == UINT64_MAX || retry <= cand) return 0;
        cand = retry;
    }
    if (p->first == 0) p->first = cand;
    p->next = p->end = cand + len;
    return cand;
}
