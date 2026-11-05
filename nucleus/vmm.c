/* vmm.c - see vmm.h. TLB: every change to a present entry is shot down on
 * every CPU that may cache it (tlb.c, W20-S07) before the call returns, and
 * before a frame it freed can be handed out again. Page-table changes hold
 * g_vm_lock (every address space; W20-S06), taken after an address space's
 * own lock and before the frame allocator's. Never frees a frame that a CPU
 * might still reach through a stale entry. */
#include "vmm.h"
#include "spinlock.h"
#include "tlb.h"
#include "vm_region.h"
#include "frames.h"
#include "boot.h"
#include "cpu.h"
#include "kprintf.h"
#include "panic.h"

#define CR0_WP (1ull << 16)

static struct addr_space g_kernel;
static uint64_t g_nx_mask = ~0ull;          /* clears PTE_NX when the CPU lacks NX */

static uint64_t pt_alloc(void *env) { (void)env; return frame_alloc(); }
static void *pt_ptr(void *env, uint64_t pa) { (void)env; return phys_to_virt(pa); }

struct addr_space *vmm_kernel(void) { return &g_kernel; }

static void flush(const struct addr_space *as, uint64_t va, uint64_t len) {
    tlb_shootdown(as->pt.root, va, len);
}

/* Frames whose entries are already clear, freed only after one shootdown
 * covers them all (a 2 MiB leaf is one entry of 512 frames). */
#define FREE_BATCH 64u
struct free_batch {
    const struct addr_space *as;
    uint64_t lo, hi;                  /* the range the entries came from */
    uint32_t n;
    uint64_t pa[FREE_BATCH], pages_bytes[FREE_BATCH];
};

static void batch_flush(struct free_batch *b) {
    if (b->hi > b->lo) flush(b->as, b->lo, b->hi - b->lo);
    for (uint32_t i = 0; i < b->n; i++)
        for (uint64_t off = 0; off < b->pages_bytes[i]; off += PAGE_4K) frame_free(b->pa[i] + off);
    b->n = 0;
    b->lo = b->hi = 0;
}

static void batch_add(struct free_batch *b, uint64_t va, uint64_t pa, uint64_t page) {
    if (b->hi == b->lo) b->lo = va;
    b->hi = va + page;
    if (pa) {
        b->pa[b->n] = pa;
        b->pages_bytes[b->n++] = page;
    }
    if (b->n == FREE_BATCH) batch_flush(b);
}

void vmm_init(void) {
    g_kernel.pt = (struct pt_ctx){ pt_alloc, pt_ptr, 0, read_cr3() & PTE_ADDR_MASK };
    uint64_t *pml4 = phys_to_virt(g_kernel.pt.root);
    pml4[0] = 0;                              /* loader's identity map: nothing below uses it */
    write_cr3(g_kernel.pt.root);              /* flush every non-global entry */
    tlb_set_loaded(g_kernel.pt.root);
    write_cr0(read_cr0() | CR0_WP);
    if (!(rdmsr(MSR_EFER) & EFER_NXE)) g_nx_mask = ~PTE_NX;   /* bit 63 would be reserved */
    vm_region_init(&g_kernel);
    kprintf("[nucleus] vmm ok (cr3=%llx, identity map dropped, wp=on, nx=%s)\n",
            (unsigned long long)g_kernel.pt.root, g_nx_mask == ~0ull ? "on" : "off");
}

/* A process's address space: an empty lower half and the kernel's upper
 * half. The upper PML4 entries point at the kernel's own PDPTs, so later
 * kernel mappings inside them (thread stacks, MMIO) appear everywhere; the
 * kernel never adds an upper-half PML4 entry after boot. */
int vm_as_init_user(struct addr_space *as) {
    uint64_t root = frame_alloc_zeroed();
    if (!root) return -1;
    uint64_t *pml4 = phys_to_virt(root);
    const uint64_t *kernel = phys_to_virt(g_kernel.pt.root);
    for (int i = PML4_KERNEL_FIRST; i < 512; i++) pml4[i] = kernel[i];
    as->pt = (struct pt_ctx){ pt_alloc, pt_ptr, 0, root };
    return 0;
}

/* Frees one table and, below the PDPT level, the tables it points to.
 * Leaves (and 2 MiB pages) are left alone: their frames are not ours. */
static void free_table(uint64_t pa, int level) {
    uint64_t *t = phys_to_virt(pa);
    if (level > 1)
        for (int i = 0; i < 512; i++)
            if ((t[i] & PTE_P) && !(t[i] & PTE_PS)) free_table(t[i] & PTE_ADDR_MASK, level - 1);
    frame_free(pa);
}

void vm_as_free_tables(struct addr_space *as) {
    uint64_t *pml4 = phys_to_virt(as->pt.root);
    for (int i = 0; i < PML4_KERNEL_FIRST; i++)
        if (pml4[i] & PTE_P) free_table(pml4[i] & PTE_ADDR_MASK, 3);   /* PDPT, PD, PT */
    frame_free(as->pt.root);
    as->pt.root = 0;
}

static struct spinlock g_vm_lock = SPINLOCK_INIT("vm");

enum pt_status vm_map(struct addr_space *as, uint64_t va, uint64_t pa, uint64_t len, uint64_t flags) {
    uint64_t f = spin_lock_irqsave(&g_vm_lock);
    enum pt_status st = pt_map(&as->pt, va, pa, len, flags & g_nx_mask);
    spin_unlock_irqrestore(&g_vm_lock, f);
    return st;        /* new entries need no flush: nothing was cached for them */
}

struct unmap_env { struct free_batch b; int free_frames; };

/* Per removed leaf: the entry is already clear; its frame waits in the batch
 * until every CPU has flushed it, or a stale TLB entry would alias the new owner. */
static void on_unmap(void *env, uint64_t va, uint64_t pa, uint64_t page) {
    struct unmap_env *u = env;
    batch_add(&u->b, va, u->free_frames ? pa : 0, page);
}

enum pt_status vm_unmap(struct addr_space *as, uint64_t va, uint64_t len, int free_frames) {
    struct unmap_env u = { .b = { .as = as }, .free_frames = free_frames };
    uint64_t f = spin_lock_irqsave(&g_vm_lock);
    enum pt_status st = pt_unmap(&as->pt, va, len, on_unmap, &u);
    batch_flush(&u.b);
    spin_unlock_irqrestore(&g_vm_lock, f);
    return st;
}

enum pt_status vm_protect(struct addr_space *as, uint64_t va, uint64_t len, uint64_t flags) {
    uint64_t f = spin_lock_irqsave(&g_vm_lock);
    enum pt_status st = pt_protect(&as->pt, va, len, flags & g_nx_mask);
    flush(as, va, len);
    spin_unlock_irqrestore(&g_vm_lock, f);
    return st;
}

static uint64_t count_present(uint64_t table_pa, int level) {
    const uint64_t *t = phys_to_virt(table_pa);
    uint64_t n = 0;
    for (int i = 0; i < 512; i++) {
        if (!(t[i] & PTE_P)) continue;
        if (level == 0) n++;
        else if (t[i] & PTE_PS) n += 512;                 /* a 2 MiB page */
        else n += count_present(t[i] & PTE_ADDR_MASK, level - 1);
    }
    return n;
}

uint64_t vm_lower_half_pages(const struct addr_space *as) {
    const uint64_t *pml4 = phys_to_virt(as->pt.root);
    uint64_t n = 0;
    for (int i = 0; i < PML4_KERNEL_FIRST; i++)
        if (pml4[i] & PTE_P) n += count_present(pml4[i] & PTE_ADDR_MASK, 2);
    return n;
}

/* Frees a mapped page's frame, leaving a zero-fill-on-demand marker. */
enum pt_status vm_discard_page(struct addr_space *as, uint64_t va) {
    return vm_discard_pages(as, va, PAGE_4K) ? PT_OK : PT_NOT_MAPPED;
}

/* Every mapped page of [va, va+len) discarded, with one shootdown per
 * FREE_BATCH frames (madvise(DONTNEED) of a large range). */
uint64_t vm_discard_pages(struct addr_space *as, uint64_t va, uint64_t len) {
    struct free_batch b = { .as = as };
    uint64_t done = 0, f = spin_lock_irqsave(&g_vm_lock);
    for (uint64_t off = 0; off < len; off += PAGE_4K) {
        uint64_t old;
        if (pt_discard(&as->pt, va + off, &old) != PT_OK) continue;   /* not mapped: nothing to do */
        batch_add(&b, va + off, old, PAGE_4K);
        done++;
    }
    batch_flush(&b);                     /* before the frames can be reused */
    spin_unlock_irqrestore(&g_vm_lock, f);
    return done;
}

enum pt_status vm_mark_lazy(struct addr_space *as, uint64_t va, uint64_t flags) {
    uint64_t f = spin_lock_irqsave(&g_vm_lock);
    enum pt_status st = pt_mark_lazy(&as->pt, va, flags & g_nx_mask);
    spin_unlock_irqrestore(&g_vm_lock, f);
    return st;         /* nothing was present: nothing to flush */
}

void vm_forget_discarded(struct addr_space *as, uint64_t va) {
    uint64_t f = spin_lock_irqsave(&g_vm_lock);
    pt_forget_discarded(&as->pt, va);
    spin_unlock_irqrestore(&g_vm_lock, f);
}

int vm_fault_in(struct addr_space *as, uint64_t va) {
    va &= ~(PAGE_4K - 1);
    uint64_t flags, f = spin_lock_irqsave(&g_vm_lock);
    int done = 0;
    uint64_t pa_now;
    if (pt_translate(&as->pt, va, &pa_now, &flags) == PT_OK) {
        done = 1;          /* BUG-0027: another CPU filled it since our fault; retry the access */
    } else if (pt_discarded(&as->pt, va, &flags)) {
        uint64_t pa = frame_alloc_zeroed();
        done = -1;                                 /* marked, but no frame to give it */
        if (pa && pt_map(&as->pt, va, pa, PAGE_4K, flags & g_nx_mask) == PT_OK) done = 1;
        else if (pa) frame_free(pa);
    }
    spin_unlock_irqrestore(&g_vm_lock, f);
    return done;
}

enum pt_status vm_translate(const struct addr_space *as, uint64_t va, uint64_t *pa, uint64_t *flags) {
    return pt_translate(&as->pt, va, pa, flags);
}

void *vm_map_mmio(uint64_t pa, uint64_t len) {
    uint64_t va = boot_info()->physmap_base + pa;
    enum pt_status st = vm_map(&g_kernel, va, pa, len, PTE_W | PTE_PCD | PTE_PWT | PTE_NX);
    if (st != PT_OK && st != PT_ALREADY_MAPPED)
        panic("vm_map_mmio %llx: %s", (unsigned long long)pa, pt_status_str(st));
    return (void *)(uintptr_t)va;
}
