/* vmm.c - see vmm.h. TLB: entries are flushed with invlpg when the address
 * space is the one in CR3; a later switch to another CR3 flushes anyway. */
#include "vmm.h"
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

static int is_current(const struct addr_space *as) {
    return (read_cr3() & PTE_ADDR_MASK) == as->pt.root;
}

static void flush(const struct addr_space *as, uint64_t va, uint64_t len) {
    if (!is_current(as)) return;
    for (uint64_t off = 0; off < len; off += PAGE_4K) invlpg(va + off);
}

void vmm_init(void) {
    g_kernel.pt = (struct pt_ctx){ pt_alloc, pt_ptr, 0, read_cr3() & PTE_ADDR_MASK };
    uint64_t *pml4 = phys_to_virt(g_kernel.pt.root);
    pml4[0] = 0;                              /* loader's identity map: nothing below uses it */
    write_cr3(g_kernel.pt.root);              /* flush every non-global entry */
    write_cr0(read_cr0() | CR0_WP);
    if (!(rdmsr(MSR_EFER) & EFER_NXE)) g_nx_mask = ~PTE_NX;   /* bit 63 would be reserved */
    vm_region_init(&g_kernel);
    kprintf("[nucleus] vmm ok (cr3=%llx, identity map dropped, wp=on, nx=%s)\n",
            (unsigned long long)g_kernel.pt.root, g_nx_mask == ~0ull ? "on" : "off");
}

enum pt_status vm_map(struct addr_space *as, uint64_t va, uint64_t pa, uint64_t len, uint64_t flags) {
    uint64_t f = irq_save();
    enum pt_status st = pt_map(&as->pt, va, pa, len, flags & g_nx_mask);
    irq_restore(f);
    return st;        /* new entries need no flush: nothing was cached for them */
}

struct unmap_env { const struct addr_space *as; int free_frames; };

/* Per removed leaf: the entry is already clear; flush it BEFORE the frame can
 * be handed out again, or a stale TLB entry would alias the new owner. */
static void on_unmap(void *env, uint64_t va, uint64_t pa, uint64_t page) {
    const struct unmap_env *u = env;
    flush(u->as, va, page);
    if (u->free_frames)
        for (uint64_t off = 0; off < page; off += PAGE_4K) frame_free(pa + off);
}

enum pt_status vm_unmap(struct addr_space *as, uint64_t va, uint64_t len, int free_frames) {
    struct unmap_env u = { as, free_frames };
    uint64_t f = irq_save();
    enum pt_status st = pt_unmap(&as->pt, va, len, on_unmap, &u);
    irq_restore(f);
    return st;
}

enum pt_status vm_protect(struct addr_space *as, uint64_t va, uint64_t len, uint64_t flags) {
    uint64_t f = irq_save();
    enum pt_status st = pt_protect(&as->pt, va, len, flags & g_nx_mask);
    flush(as, va, len);
    irq_restore(f);
    return st;
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
