/* go_load.c - see go_load.h. The Go kernel shares the nucleus's address space
 * (the kernel PML4); its segments and stack are ordinary reservations, so
 * mmap can never hand those addresses out again. */
#include "go_load.h"
#include "go_stack.h"
#include "elf64.h"
#include "boot.h"
#include "frames.h"
#include "vmm.h"
#include "vm_region.h"
#include "sched.h"
#include "syscall.h"
#include "trap.h"
#include "gdt.h"
#include "cpu.h"
#include "kprintf.h"
#include "lib_string.h"
#include "panic.h"

/* Go's rt0_go assumes 64 KiB of g0 stack BELOW the entry SP (Linux gives
 * 8 MiB); argv/env/auxv sit above it, so give m0 well over that. */
#define GO_STACK_BYTES (1024ull * 1024)
#define PF_X 1u
#define PF_W 2u
#define RFLAGS_RESERVED1 (1ull << 1)

static const char *const g_env[] = {
    "GODEBUG=asyncpreemptoff=1",   /* no signals -> no async preemption (PIT-06) */
    "GOTRACEBACK=all",             /* full goroutine dumps on panic              */
    0,
};

static uint64_t page_down(uint64_t v) { return v & ~(PAGE_4K - 1); }
static uint64_t page_up(uint64_t v)   { return (v + PAGE_4K - 1) & ~(PAGE_4K - 1); }

/* One page of a segment: back it (or reuse the frame if a previous segment
 * already mapped it), copy the file bytes that fall inside it. */
static void map_segment_page(struct addr_space *as, const uint8_t *img, const struct elf64_segment *s,
                             uint64_t page, uint64_t flags) {
    uint64_t pa, old;
    if (vm_translate(as, page, &pa, &old) == PT_OK) {
        pa &= ~(PAGE_4K - 1);                      /* shared page: union of permissions */
        uint64_t merged = (old & PTE_W) | (flags & PTE_W) | ((old & flags) & PTE_NX);
        if (vm_protect(as, page, PAGE_4K, merged) != PT_OK) panic("go: cannot re-protect shared page");
    } else {
        pa = frame_alloc_zeroed();
        if (!pa || vm_map(as, page, pa, PAGE_4K, flags) != PT_OK) panic("go: out of memory for segments");
    }
    uint64_t lo = page > s->vaddr ? page : s->vaddr;
    uint64_t hi = page + PAGE_4K < s->vaddr + s->filesz ? page + PAGE_4K : s->vaddr + s->filesz;
    if (hi > lo) memcpy((uint8_t *)phys_to_virt(pa) + (lo - page), img + s->offset + (lo - s->vaddr), hi - lo);
}

/* Maps every PT_LOAD; fills ex (entry, phdr) and the gate's text range. */
static void map_go_elf(struct addr_space *as, const uint8_t *img, uint64_t len, struct exec_info *ex) {
    struct elf64_info info;
    enum elf_status st = elf64_inspect(img, len, &info);
    if (st != ELF_OK) panic("go kernel ELF: %s", elf_status_str(st));
    const uint64_t phoff = *(const uint64_t *)(img + 32);           /* e_phoff */
    *ex = (struct exec_info){ 0, 56, info.phnum, info.entry };
    for (uint16_t i = 0; i < info.phnum; i++) {
        struct elf64_segment s;
        if ((st = elf64_segment_at(img, len, i, &s)) != ELF_OK) panic("go kernel ELF: %s", elf_status_str(st));
        if (s.memsz == 0) continue;
        uint64_t lo = page_down(s.vaddr), hi = page_up(s.vaddr + s.memsz), got;
        if (!regions_covers(&as->regions, lo, hi - lo) &&
            (vm_reserve(as, lo, hi - lo, &got) != 0 || got != lo))
            panic("go: segment %llx..%llx is not free", (unsigned long long)lo, (unsigned long long)hi);
        uint64_t flags = (s.flags & PF_W ? PTE_W : 0) | (s.flags & PF_X ? 0 : PTE_NX);
        for (uint64_t page = lo; page < hi; page += PAGE_4K) map_segment_page(as, img, &s, page, flags);
        if (s.flags & PF_X) { go_text_lo = s.vaddr; go_text_hi = s.vaddr + s.memsz; }
        if (phoff >= s.offset && phoff < s.offset + s.filesz) ex->phdr = s.vaddr + (phoff - s.offset);
        kprintf("  go segment %016llx - %016llx %c%c%c\n", (unsigned long long)s.vaddr,
                (unsigned long long)(s.vaddr + s.memsz), 'r', s.flags & PF_W ? 'w' : '-', s.flags & PF_X ? 'x' : '-');
    }
    if (!go_text_hi) panic("go kernel ELF has no executable segment");
}

/* The m0 stack plus an unmapped guard page below it. Returns its top. */
static uint64_t map_go_stack(struct addr_space *as) {
    uint64_t base;
    if (vm_reserve(as, 0, GO_STACK_BYTES + PAGE_4K, &base) != 0) panic("go: no room for the stack");
    if (vm_commit(as, base + PAGE_4K, GO_STACK_BYTES, VM_PROT_READ | VM_PROT_WRITE) != 0)
        panic("go: out of memory for the stack");
    return base + PAGE_4K + GO_STACK_BYTES;
}

/* argv = "kernel" plus the words of an optional "kargs" boot module (a text
 * file on the image; tests use it to choose what the Go kernel does). */
#define KARGS_BYTES 256
static char g_kargs[KARGS_BYTES];

static void kernel_argv(const char **argv) {
    unsigned n = 0;
    argv[n++] = "kernel";
    const struct boot_module *m = boot_module_find("kargs");
    if (m) {
        uint64_t len = m->size_bytes < KARGS_BYTES - 1 ? m->size_bytes : KARGS_BYTES - 1;
        memcpy(g_kargs, phys_to_virt(m->phys_start), len);
        g_kargs[len] = 0;
        n += split_words(g_kargs, argv + n, STACK_MAX_ARGS - n);
        kprintf("[nucleus] go kernel args from kargs: %u word(s)\n", n - 1);
    }
    argv[n] = 0;
}

int go_kernel_start(void) {
    const struct boot_module *mod = boot_module_find("kernel");
    if (!mod) return -1;
    struct addr_space *as = vmm_kernel();
    struct exec_info ex;
    map_go_elf(as, phys_to_virt(mod->phys_start), mod->size_bytes, &ex);

    uint8_t rnd[16];
    for (int i = 0; i < 16; i += 8) { uint64_t t = rdtsc() * 0x9E3779B97F4A7C15ull; memcpy(rnd + i, &t, 8); }
    const char *argv[STACK_MAX_ARGS + 1];
    kernel_argv(argv);
    uint64_t sp = build_initial_stack(map_go_stack(as), &ex, rnd, argv, g_env);

    struct trap_frame f;
    memset(&f, 0, sizeof f);                      /* RDX = 0: no atexit function (ELF ABI) */
    f.rip = ex.entry;
    f.cs = SEL_KCODE;
    f.rflags = RFLAGS_IF | RFLAGS_RESERVED1;
    f.rsp = sp;
    f.ss = SEL_KDATA;
    if (!thread_create_from_frame("go-m0", &f, 0)) panic("go: cannot create thread m0");
    kprintf("[nucleus] go kernel mapped (entry=%llx, text %llx..%llx, rsp=%llx)\n",
            (unsigned long long)ex.entry, (unsigned long long)go_text_lo,
            (unsigned long long)go_text_hi, (unsigned long long)sp);
    return 0;
}
