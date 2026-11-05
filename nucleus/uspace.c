/* uspace.c - see uspace.h. One lock for the handle table; page-table edits
 * themselves are serialised by vmm.c (IRQs off around each walk). */
#include "pat.h"
#include "uspace.h"
#include "vmm.h"
#include "frames.h"
#include "spinlock.h"
#include "errno.h"

struct uspace {
    int used;
    uint32_t threads;          /* user threads whose CR3 this is */
    struct addr_space as;
};

static struct uspace g_spaces[USPACE_MAX];
static struct spinlock g_lock = SPINLOCK_INIT("uspace");

static struct uspace *get(uint64_t h) {
    if (h == 0 || h > USPACE_MAX || g_spaces[h - 1].used != 1) return 0;   /* -1: being destroyed */
    return &g_spaces[h - 1];
}

struct addr_space *uspace_get(uint64_t h) {
    struct uspace *u = get(h);
    return u ? &u->as : 0;
}

void uspace_thread_ref(uint64_t h, int delta) {
    uint64_t f = spin_lock_irqsave(&g_lock);
    struct uspace *u = get(h);
    if (u) u->threads += (uint32_t)delta;
    spin_unlock_irqrestore(&g_lock, f);
}

long uspace_create(void) {
    uint64_t f = spin_lock_irqsave(&g_lock);
    uint64_t h = 0;
    for (uint32_t i = 0; i < USPACE_MAX && !h; i++)
        if (!g_spaces[i].used) { g_spaces[i].used = 1; g_spaces[i].threads = 0; h = i + 1; }
    spin_unlock_irqrestore(&g_lock, f);
    if (!h) return -EMFILE;
    if (vm_as_init_user(&g_spaces[h - 1].as) != 0) {
        g_spaces[h - 1].used = 0;
        return -ENOMEM;
    }
    return (long)h;
}

static int user_range(uint64_t va, uint64_t len) {
    return len && !(va & 0xFFF) && !(len & 0xFFF) && va < USER_VA_TOP && len <= USER_VA_TOP - va;
}

static long status(enum pt_status st) {
    switch (st) {
    case PT_OK:             return 0;
    case PT_NO_MEMORY:      return -ENOMEM;
    case PT_ALREADY_MAPPED: return -EEXIST;
    case PT_NOT_MAPPED:     return -EFAULT;
    default:                return -EINVAL;
    }
}

#define UPROT_ALL (UPROT_WRITE | UPROT_EXEC | UPROT_WC)

/* A user page's entry bits; UPROT_WC gives device memory the same cache
 * type as the kernel's own mapping of it (PIT-64), uncached without PAT. */
static uint64_t pte_flags(uint64_t prot) {
    return PTE_U | (prot & UPROT_WRITE ? PTE_W : 0) | (prot & UPROT_EXEC ? 0 : PTE_NX) |
           (prot & UPROT_WC ? pat_wc_flags() : 0);
}

long uspace_map(uint64_t h, uint64_t va, uint64_t pa, uint64_t len, uint64_t prot) {
    struct uspace *u = get(h);
    if (!u) return -EBADF;
    if (!user_range(va, len) || (pa & 0xFFF) || prot & ~(uint64_t)UPROT_ALL) return -EINVAL;
    return status(vm_map(&u->as, va, pa, len, pte_flags(prot)));
}

long uspace_unmap(uint64_t h, uint64_t va, uint64_t len) {
    struct uspace *u = get(h);
    if (!u) return -EBADF;
    if (!user_range(va, len)) return -EINVAL;
    return status(vm_unmap(&u->as, va, len, 0));
}


/* Every mapped page of [va, va+len) gets prot, its frame kept, the entry
 * rewritten in place (vm_protect); pages not mapped are skipped. One call
 * where fork needed two per page. */
long uspace_protect(uint64_t h, uint64_t va, uint64_t len, uint64_t prot) {
    struct uspace *u = get(h);
    if (!u) return -EBADF;
    if (!user_range(va, len) || prot & ~(uint64_t)UPROT_ALL) return -EINVAL;
    if (vm_protect(&u->as, va, len, pte_flags(prot)) == PT_OK) return 0;   /* a run with no holes */
    for (uint64_t off = 0; off < len; off += 4096) {                        /* holes: page by page */
        uint64_t pa, flags;
        if (vm_translate(&u->as, va + off, &pa, &flags) != PT_OK) continue;
        enum pt_status st = vm_protect(&u->as, va + off, 4096, pte_flags(prot));
        if (st != PT_OK) return status(st);
    }
    return 0;
}

/* n single pages from an array the caller (Go) owns. Checked as nu_as_map
 * checks one; on an error the pages mapped so far stay (the caller unmaps). */
long uspace_map_batch(uint64_t h, const struct uspace_map_entry *e, uint64_t n) {
    struct uspace *u = get(h);
    if (!u) return -EBADF;
    if (n > USPACE_BATCH_MAX) return -EINVAL;
    for (uint64_t i = 0; i < n; i++) {
        if (!user_range(e[i].va, 4096) || (e[i].pa & 0xFFF) || e[i].prot & ~(uint64_t)UPROT_ALL)
            return -EINVAL;
        enum pt_status st = vm_map(&u->as, e[i].va, e[i].pa, 4096, pte_flags(e[i].prot));
        if (st != PT_OK) return status(st);
    }
    return 0;
}

long uspace_translate(uint64_t h, uint64_t va) {
    struct uspace *u = get(h);
    if (!u) return -EBADF;
    uint64_t pa, flags;
    if (va >= USER_VA_TOP || vm_translate(&u->as, va, &pa, &flags) != PT_OK || !(flags & PTE_U))
        return -EFAULT;
    return (long)pa;
}

long uspace_destroy(uint64_t h) {
    uint64_t f = spin_lock_irqsave(&g_lock);
    struct uspace *u = get(h);
    long rc = !u ? -EBADF : u->threads ? -EBUSY : 0;
    if (rc == 0) u->used = -1;                        /* nobody can look it up any more */
    spin_unlock_irqrestore(&g_lock, f);
    if (rc != 0) return rc;
    vm_as_free_tables(&u->as);
    u->used = 0;
    return 0;
}

long uspace_page_alloc(uint64_t count) {
    if (count != 1) return -EINVAL;
    if (frames_free_count() <= USER_RESERVE_FRAMES) return -ENOMEM;
    uint64_t pa = frame_alloc_zeroed();
    return pa ? (long)pa : -ENOMEM;
}

long uspace_page_free(uint64_t pa, uint64_t count) {
    if (count != 1 || (pa & 0xFFF) || !pa) return -EINVAL;
    frame_free(pa);
    return 0;
}
