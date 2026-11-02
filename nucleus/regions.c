#include "regions.h"

void regions_init(struct regions *rs, uint64_t lo, uint64_t hi, uint64_t search_base) {
    rs->lo = lo; rs->hi = hi; rs->search_base = search_base; rs->count = 0;
}

static int range_free(const struct regions *rs, uint64_t start, uint64_t len) {
    if (start < rs->lo || start > rs->hi || len > rs->hi - start) return 0;
    for (uint32_t i = 0; i < rs->count; i++)
        if (start < rs->r[i].start + rs->r[i].len && rs->r[i].start < start + len) return 0;
    return 1;
}

static int insert(struct regions *rs, uint64_t start, uint64_t len) {
    if (rs->count == REGIONS_MAX) return -1;
    uint32_t at = 0;
    while (at < rs->count && rs->r[at].start < start) at++;
    for (uint32_t i = rs->count; i > at; i--) rs->r[i] = rs->r[i - 1];
    rs->r[at].start = start;
    rs->r[at].len = len;
    rs->count++;
    return 0;
}

int regions_reserve(struct regions *rs, uint64_t hint, uint64_t len, uint64_t *out) {
    if (len == 0) return -1;
    uint64_t cand = hint && range_free(rs, hint, len) ? hint : 0;
    if (!cand) {
        cand = rs->search_base;
        for (uint32_t i = 0; i < rs->count; i++) {         /* sorted: walk gaps upward */
            const struct region *g = &rs->r[i];
            if (g->start + g->len <= cand) continue;
            if (g->start >= cand && g->start - cand >= len) break;
            cand = g->start + g->len;
        }
        if (!range_free(rs, cand, len)) return -1;
    }
    if (insert(rs, cand, len) != 0) return -1;
    *out = cand;
    return 0;
}

int regions_covers(const struct regions *rs, uint64_t addr, uint64_t len) {
    uint64_t end = addr + len;
    if (end < addr) return 0;
    for (uint32_t i = 0; i < rs->count && addr < end; i++) {   /* sorted: consume in order */
        const struct region *g = &rs->r[i];
        if (g->start + g->len <= addr) continue;
        if (g->start > addr) return 0;
        addr = g->start + g->len;
    }
    return addr >= end;
}

int regions_release(struct regions *rs, uint64_t addr, uint64_t len) {
    uint64_t end = addr + len;
    for (uint32_t i = 0; i < rs->count;) {
        struct region *g = &rs->r[i];
        uint64_t gs = g->start, ge = g->start + g->len;
        if (ge <= addr || gs >= end) { i++; continue; }
        if (gs < addr && ge > end) {                       /* hole in the middle: split */
            g->len = addr - gs;
            return insert(rs, end, ge - end);
        }
        if (gs < addr)      { g->len = addr - gs; i++; continue; }  /* keep the head */
        if (ge > end)       { g->start = end; g->len = ge - end; i++; continue; } /* keep the tail */
        for (uint32_t j = i; j + 1 < rs->count; j++) rs->r[j] = rs->r[j + 1];  /* gone whole */
        rs->count--;
    }
    return 0;
}
