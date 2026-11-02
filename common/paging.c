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

static enum pt_status check_va_range(uint64_t va, uint64_t size) {
    if (size == 0 || ((va | size) & (PAGE_4K - 1))) return PT_UNALIGNED;
    uint64_t va_last = va + size - 1;
    if (va_last < va || !canonical(va) || !canonical(va_last) || (va >> 63) != (va_last >> 63))
        return PT_NON_CANONICAL;
    return PT_OK;
}

/* Pointer to the leaf entry that maps va (and its page size), or 0. */
static uint64_t *leaf_entry(const struct pt_ctx *c, uint64_t va, uint64_t *page) {
    uint64_t *t = table_at(c, c->root);
    for (unsigned level = 3;; level--) {
        uint64_t *e = &t[idx(va, level)];
        if (!(*e & PTE_P)) return 0;
        if (level == 0 || (level == 1 && (*e & PTE_PS))) {
            *page = level ? PAGE_2M : PAGE_4K;
            return e;
        }
        t = table_at(c, *e & PTE_ADDR_MASK);
    }
}

#define LEAF_FLAGS (PTE_W | PTE_U | PTE_PWT | PTE_PCD | PTE_G | PTE_NX)

enum pt_status pt_map(struct pt_ctx *c, uint64_t va, uint64_t pa, uint64_t size, uint64_t flags) {
    enum pt_status st0 = check_va_range(va, size);
    if (st0 != PT_OK) return st0;
    if (pa & (PAGE_4K - 1)) return PT_UNALIGNED;
    uint64_t pa_last = pa + size - 1;
    if (pa_last < pa || pa_last >= PHYS_LIMIT) return PT_BAD_PHYS;

    uint64_t leaf = PTE_P | (flags & LEAF_FLAGS);
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
    uint64_t page;
    const uint64_t *e = leaf_entry(c, va, &page);
    if (!e) return PT_NOT_MAPPED;
    *pa = ((*e & PTE_ADDR_MASK) & ~(page - 1)) | (va & (page - 1));
    *flags = *e & ~PTE_ADDR_MASK;
    return PT_OK;
}

/* Shared walk for unmap/protect: every page in the range must be mapped, and
 * a 2 MiB leaf must be covered whole (no splitting). */
static enum pt_status for_each_leaf(struct pt_ctx *c, uint64_t va, uint64_t size,
                                    enum pt_status (*fn)(uint64_t *e, uint64_t va, uint64_t page, void *arg),
                                    void *arg) {
    enum pt_status st = check_va_range(va, size);
    if (st != PT_OK) return st;
    while (size) {
        uint64_t page;
        uint64_t *e = leaf_entry(c, va, &page);
        if (!e) return PT_NOT_MAPPED;
        if ((va & (page - 1)) || size < page) return PT_UNALIGNED;
        if ((st = fn(e, va, page, arg)) != PT_OK) return st;
        va += page; size -= page;
    }
    return PT_OK;
}

struct unmap_arg { void (*on_unmap)(void *env, uint64_t va, uint64_t pa, uint64_t page); void *env; };

static enum pt_status unmap_leaf(uint64_t *e, uint64_t va, uint64_t page, void *arg) {
    const struct unmap_arg *u = arg;
    uint64_t pa = (*e & PTE_ADDR_MASK) & ~(page - 1);
    *e = 0;
    if (u->on_unmap) u->on_unmap(u->env, va, pa, page);
    return PT_OK;
}

enum pt_status pt_unmap(struct pt_ctx *c, uint64_t va, uint64_t size,
                        void (*on_unmap)(void *env, uint64_t va, uint64_t pa, uint64_t page), void *env) {
    struct unmap_arg u = { on_unmap, env };
    return for_each_leaf(c, va, size, unmap_leaf, &u);
}

static enum pt_status protect_leaf(uint64_t *e, uint64_t va, uint64_t page, void *arg) {
    (void)va; (void)page;
    *e = (*e & (PTE_ADDR_MASK | PTE_PS)) | PTE_P | (*(const uint64_t *)arg & LEAF_FLAGS);
    return PT_OK;
}

enum pt_status pt_protect(struct pt_ctx *c, uint64_t va, uint64_t size, uint64_t flags) {
    return for_each_leaf(c, va, size, protect_leaf, &flags);
}

const char *pt_status_str(enum pt_status s) {
    static const char *const names[] = {
        "ok", "out of memory", "unaligned", "non-canonical", "bad physical address",
        "already mapped", "not mapped",
    };
    return (unsigned)s < sizeof names / sizeof names[0] ? names[s] : "unknown";
}
