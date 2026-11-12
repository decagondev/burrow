/*
 * handoff.c - see handoff.h. Moved out of loader/main.c (W4-W21) so the
 * UEFI loader (W26-S03) starts the nucleus the same way: the same segment
 * checks, the same page tables (vmsetup.c), the same boot_info v4.
 *
 * RULE: no firmware calls, no disk; frames only from h->frames, so
 * "loader-frames" covers every page table the nucleus inherits.
 * Docs: handoff.h, docs/02-architecture/BOOT_PROTOCOL.md.
 */
#include "handoff.h"
#include "io.h"
#include "cpu.h"
#include "kprintf.h"
#include "lib_string.h"
#include "layout.h"
#include "paging.h"
#include "vmsetup.h"
#include "manifest.h"

void handoff_panic(const struct handoff *h, const char *what) {
    kprintf("[%s] PANIC: %s\n", h->tag, what);
    qemu_exit(QEMU_EXIT_FAILURE);
    cpu_halt_forever();
}

int handoff_is_boot_stage(const char *name) {
    return strncmp(name, "stage2", MANIFEST_NAME_LEN) == 0 || strncmp(name, "loader", MANIFEST_NAME_LEN) == 0 ||
           strncmp(name, "nucleus", MANIFEST_NAME_LEN) == 0;
}

void handoff_add_module(struct handoff *h, const char *name, uint64_t start, uint64_t size_bytes) {
    if (h->module_count == BOOT_MODULES_MAX) handoff_panic(h, "too many boot modules");
    struct boot_module *mod = &h->modules[h->module_count++];
    size_t len = strlen(name);
    if (len >= sizeof mod->name) handoff_panic(h, "boot module name too long");
    memset(mod->name, 0, sizeof mod->name);
    memcpy(mod->name, name, len);
    mod->phys_start = start;
    mod->size_bytes = size_bytes;
}

void handoff_load_nucleus(struct handoff *h, const uint8_t *img, uint64_t size_bytes, struct elf_loaded *out) {
    struct load_window win = { NUCLEUS_PHYS_BASE, NUCLEUS_PHYS_LIMIT, (uint8_t *)(uintptr_t)NUCLEUS_PHYS_BASE };
    enum elf_status st = elf64_load(img, size_bytes, &win, out);
    if (st != ELF_OK) handoff_panic(h, elf_status_str(st));
    for (uint32_t i = 0; i < out->count; i++) {
        const struct elf64_segment *s = &out->seg[i];
        if (s->vaddr < NUCLEUS_VIRT_BASE) handoff_panic(h, "nucleus segment below the top 2 GiB");
        kprintf("  segment %016llx -> phys %08llx  %6llu bytes  %c%c%c\n",
                (unsigned long long)s->vaddr, (unsigned long long)s->paddr,
                (unsigned long long)s->memsz, s->flags & 4 ? 'r' : '-',
                s->flags & 2 ? 'w' : '-', s->flags & 1 ? 'x' : '-');
    }
    kprintf("[%s] nucleus loaded entry=%llx\n", h->tag, (unsigned long long)out->entry);
}

static uint64_t frame_alloc(void *env) { return palloc_alloc(env, 1); }
static void *frame_ptr(void *env, uint64_t pa) { (void)env; return (void *)(uintptr_t)pa; }

uint64_t handoff_page_tables(struct handoff *h, const struct elf_loaded *n) {
    int nx = cpu_has_nx();
    if (nx) wrmsr(MSR_EFER, rdmsr(MSR_EFER) | EFER_NXE);   /* before bit 63 is ever live (PIT-20) */
    struct pt_ctx c = { frame_alloc, frame_ptr, h->frames, 0 };
    if (pt_init(&c) != PT_OK) handoff_panic(h, "out of memory for page tables");
    struct vm_plan plan = { h->map, h->map_count, n, nx, h->loader_base, h->loader_bytes };
    const char *what;
    enum pt_status st = vm_build(&c, &plan, &what);
    if (st != PT_OK) {
        kprintf("[%s] %s: %s\n", h->tag, what, pt_status_str(st));
        handoff_panic(h, "could not build page tables");
    }
    uint64_t pa, fl;
    if (pt_translate(&c, PHYSMAP_BASE + BOOTINFO_PHYS_ADDR, &pa, &fl) != PT_OK || pa != BOOTINFO_PHYS_ADDR ||
        pt_translate(&c, n->entry, &pa, &fl) != PT_OK || (fl & PTE_NX) ||
        pt_translate(&c, h->loader_base, &pa, &fl) != PT_OK || pa != h->loader_base || (fl & PTE_NX))
        handoff_panic(h, "page tables fail their own walk");
    kprintf("[%s] page tables ok (pml4=%llx, physmap %llu MiB, nx=%s)\n", h->tag,
            (unsigned long long)c.root,
            (unsigned long long)(vm_physmap_size(h->map, h->map_count) >> 20), nx ? "on" : "off");
    return c.root;
}

void handoff_boot_info(struct handoff *h, struct boot_info *bi, const struct elf_loaded *n) {
    uint64_t nucleus_end = NUCLEUS_PHYS_BASE;
    for (uint32_t i = 0; i < n->count; i++)
        if (n->seg[i].paddr + n->seg[i].memsz > nucleus_end) nucleus_end = n->seg[i].paddr + n->seg[i].memsz;
    nucleus_end = (nucleus_end + PAGE_SIZE - 1) & ~(uint64_t)(PAGE_SIZE - 1);
    handoff_add_module(h, "nucleus", NUCLEUS_PHYS_BASE, nucleus_end - NUCLEUS_PHYS_BASE);
    handoff_add_module(h, "loader-frames", h->frames->first, h->frames->end - h->frames->first);

    struct boot_module *mods = (struct boot_module *)(uintptr_t)BOOT_MODULES_PHYS_ADDR;
    memset(mods, 0, BOOT_MODULES_MAX * sizeof *mods);
    memcpy(mods, h->modules, h->module_count * sizeof *mods);
    bi->physmap_base = PHYSMAP_BASE;
    bi->modules_addr = BOOT_MODULES_PHYS_ADDR;
    bi->module_count = h->module_count;
    bi->version      = BOOTINFO_VERSION;
    for (uint32_t i = 0; i < bi->module_count; i++)
        kprintf("  module %08llx - %08llx  %s\n",
                (unsigned long long)mods[i].phys_start,
                (unsigned long long)(mods[i].phys_start + mods[i].size_bytes), mods[i].name);
    kprintf("[%s] boot_info v4 ok (physmap=%llx, modules=%u)\n", h->tag,
            (unsigned long long)bi->physmap_base, bi->module_count);
}

void handoff_enter(uint64_t pml4, const struct elf_loaded *n, const struct boot_info *bi) {
    cpu_enter_space(pml4, n->entry, PHYSMAP_BASE + (uint64_t)(uintptr_t)bi);
}
