#include "paging.h"
#include "lib_string.h"

#define PHYS_LIMIT (1ull << 52)

static unsigned idx(uint64_t va, unsigned level) {   /* level 3 = PML4 ... 0 = PT */
    return (unsigned)(va >> (12 + 9 * level)) & 511u;
}

static int canonical(uint64_t va) {
    uint64_t top = va >> 47;                           /* bits 63..47 all equal */
    return top == 0 || top == 0x1FFFF;
}

static uint64_t *table_at(const struct pt_ctx *c, uint64_t pa) {
    return c->phys_to_ptr(c->env, pa);
}

/* Returns the next-level table behind *entry, creating it if absent. */
static uint64_t *descend(struct pt_ctx *c, uint64_t *entry, uint64_t user, enum pt_status *st) {
    if (!(*entry & PTE_P)) {
        uint64_t pa = c->alloc_frame(c->env);
        if (!pa) { *st = PT_NO_MEMORY; return 0; }
        memset(table_at(c, pa), 0, PAGE_4K);
        *entry = pa | PTE_P | PTE_W;
    } else if (*entry & PTE_PS) {
        *st = PT_ALREADY_MAPPED;                       /* a huge page covers this range */
        return 0;
    }
    *entry |= user;                                    /* U must be set at every level */
    return table_at(c, *entry & PTE_ADDR_MASK);
}

static enum pt_status map_one(struct pt_ctx *c, uint64_t va, uint64_t pa, uint64_t leaf, int huge) {
    enum pt_status st = PT_OK;
    uint64_t user = leaf & PTE_U;
    uint64_t *t = table_at(c, c->root);
    unsigned stop = huge ? 1 : 0;                      /* 2 MiB leaves live in the PD */
    for (unsigned level = 3; level > stop; level--) {
        t = descend(c, &t[idx(va, level)], user, &st);
        if (!t) return st;
    }
    uint64_t *e = &t[idx(va, stop)];
    if (*e & PTE_P) return PT_ALREADY_MAPPED;
    *e = pa | leaf | (huge ? PTE_PS : 0);
    return PT_OK;
}

enum pt_status pt_init(struct pt_ctx *c) {
    uint64_t pa = c->alloc_frame(c->env);
    if (!pa) return PT_NO_MEMORY;
    memset(table_at(c, pa), 0, PAGE_4K);
    c->root = pa;
    return PT_OK;
}

enum pt_status pt_map(struct pt_ctx *c, uint64_t va, uint64_t pa, uint64_t size, uint64_t flags) {
    if (size == 0 || ((va | pa | size) & (PAGE_4K - 1))) return PT_UNALIGNED;
    uint64_t va_last = va + size - 1, pa_last = pa + size - 1;
    if (va_last < va || !canonical(va) || !canonical(va_last) || (va >> 63) != (va_last >> 63))
        return PT_NON_CANONICAL;
    if (pa_last < pa || pa_last >= PHYS_LIMIT) return PT_BAD_PHYS;

    uint64_t leaf = PTE_P | (flags & (PTE_W | PTE_U | PTE_G | PTE_NX));
    while (size) {
        int huge = !((va | pa) & (PAGE_2M - 1)) && size >= PAGE_2M;
        enum pt_status st = map_one(c, va, pa, leaf, huge);
        if (st != PT_OK) return st;
        uint64_t step = huge ? PAGE_2M : PAGE_4K;
        va += step; pa += step; size -= step;
    }
    return PT_OK;
}

enum pt_status pt_translate(const struct pt_ctx *c, uint64_t va, uint64_t *pa, uint64_t *flags) {
    if (!canonical(va)) return PT_NON_CANONICAL;
    uint64_t *t = table_at(c, c->root);
    for (unsigned level = 3;; level--) {
        uint64_t e = t[idx(va, level)];
        if (!(e & PTE_P)) return PT_NOT_MAPPED;
        if (level == 0 || (level == 1 && (e & PTE_PS))) {
            uint64_t page = level ? PAGE_2M : PAGE_4K;
            *pa = ((e & PTE_ADDR_MASK) & ~(page - 1)) | (va & (page - 1));
            *flags = e & ~PTE_ADDR_MASK;
            return PT_OK;
        }
        t = table_at(c, e & PTE_ADDR_MASK);
    }
}

const char *pt_status_str(enum pt_status s) {
    static const char *const names[] = {
        "ok", "out of memory", "unaligned", "non-canonical", "bad physical address",
        "already mapped", "not mapped",
    };
    return (unsigned)s < sizeof names / sizeof names[0] ? names[s] : "unknown";
}
