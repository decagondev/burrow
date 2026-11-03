/* vm_build on a synthetic 128 MiB machine: walk the result like the MMU. */
#include <stdlib.h>
#include "minitest.h"
#include "vmsetup.h"
#include "layout.h"

#define POOL_FRAMES 16
#define FAKE_PHYS   0x7000000ull
static unsigned char pool[POOL_FRAMES][4096] __attribute__((aligned(4096)));
static unsigned used;

static uint64_t pool_alloc(void *env) {
    (void)env;
    return used < POOL_FRAMES ? FAKE_PHYS + 4096ull * used++ : 0;
}
static void *pool_ptr(void *env, uint64_t pa) {
    (void)env;
    if (pa < FAKE_PHYS || pa >= FAKE_PHYS + sizeof pool) abort();
    return pool[(pa - FAKE_PHYS) / 4096];
}

static const struct e820_entry map[] = {
    { 0x0,      0x9FC00,   E820_USABLE,   1 },
    { 0x9FC00,  0x60400,   E820_RESERVED, 1 },
    { 0x100000, 0x7EE0000, E820_USABLE,   1 },   /* top = 0x7FE0000 -> physmap 128 MiB */
};
static struct elf_loaded nucleus = {
    0xFFFFFFFF80100000ull, 3, {
        { 0xFFFFFFFF80100000ull, 0x100000, 0x1000, 0x933, 0x933,  5 },   /* r-x */
        { 0xFFFFFFFF80101000ull, 0x101000, 0x2000, 0x290, 0x290,  4 },   /* r-- */
        { 0xFFFFFFFF80102000ull, 0x102000, 0,      0,     0x4020, 6 },   /* rw- */
    },
};

/* 3 GiB below the PCI hole, 1 GiB above 4 GiB (QEMU with -m 4G). */
static const struct e820_entry big[] = {
    { 0x0,         0x9FC00,     E820_USABLE,   1 },
    { 0x100000,    0xBFEE0000,  E820_USABLE,   1 },   /* top 0xBFFE0000 -> low physmap 3 GiB */
    { 0xBFFE0000,  0x20000,     E820_RESERVED, 1 },
    { 0xFFFC0000,  0x40000,     E820_RESERVED, 1 },   /* BIOS ROM, in the hole */
    { 0x100000000, 0x40000000,  E820_USABLE,   1 },
};

static struct pt_ctx build_map(const struct e820_entry *m, uint32_t n, int use_nx, enum pt_status *st) {
    struct pt_ctx c = { pool_alloc, pool_ptr, 0, 0 };
    used = 0;
    if (pt_init(&c) != PT_OK) abort();
    struct vm_plan p = { m, n, &nucleus, use_nx };
    const char *what;
    *st = vm_build(&c, &p, &what);
    return c;
}
static struct pt_ctx build(int use_nx, enum pt_status *st) { return build_map(map, 3, use_nx, st); }
static uint64_t flags_at(const struct pt_ctx *c, uint64_t va, uint64_t want_pa) {
    uint64_t pa = 0, fl = 0;
    if (pt_translate(c, va, &pa, &fl) != PT_OK || pa != want_pa) return 0;
    return fl;
}

TEST(vm_physmap_size_rounds_to_2m) { CHECK_EQ_U64(vm_physmap_size(map, 3), 0x8000000); }
TEST(vm_physmap_skips_the_pci_hole) {
    CHECK_EQ_U64(vm_physmap_size(big, 5), 0x100000000ull);       /* 3 GiB + 1 GiB */
    enum pt_status st;
    struct pt_ctx c = build_map(big, 5, 1, &st);
    CHECK(st == PT_OK);
    CHECK(flags_at(&c, PHYSMAP_BASE + 0xBFFFF000ull, 0xBFFFF000ull) & PTE_W);   /* ACPI tables */
    CHECK(flags_at(&c, PHYSMAP_BASE + 0x13FFFF000ull, 0x13FFFF000ull) & PTE_NX); /* high RAM */
    uint64_t pa, fl;
    CHECK(pt_translate(&c, PHYSMAP_BASE + 0xFD000000ull, &pa, &fl) == PT_NOT_MAPPED); /* framebuffer */
    CHECK(pt_translate(&c, PHYSMAP_BASE + 0xC0000000ull, &pa, &fl) == PT_NOT_MAPPED);
    CHECK(pt_translate(&c, PHYSMAP_BASE + 0x140000000ull, &pa, &fl) == PT_NOT_MAPPED);
}
TEST(vm_build_three_mappings) {
    enum pt_status st;
    struct pt_ctx c = build(1, &st);
    CHECK(st == PT_OK);
    CHECK_EQ_U64(used, 8);
    CHECK(flags_at(&c, 0x10000, 0x10000) & PTE_W);                         /* loader code */
    CHECK(!(flags_at(&c, 0x10000, 0x10000) & PTE_NX));
    CHECK(flags_at(&c, PHYSMAP_BASE + 0x1000, 0x1000) & PTE_NX);           /* boot_info */
    CHECK(flags_at(&c, PHYSMAP_BASE + 0x7FFFFFF, 0x7FFFFFF) & PTE_W);
    uint64_t pa, fl;
    CHECK(pt_translate(&c, PHYSMAP_BASE + 0x8000000, &pa, &fl) == PT_NOT_MAPPED);
}
TEST(vm_build_nucleus_is_w_xor_x) {
    enum pt_status st;
    struct pt_ctx c = build(1, &st);
    uint64_t text = flags_at(&c, 0xFFFFFFFF80100000ull, 0x100000);
    uint64_t ro   = flags_at(&c, 0xFFFFFFFF80101100ull, 0x101100);
    uint64_t data = flags_at(&c, 0xFFFFFFFF80106000ull, 0x106000);   /* last .bss page */
    CHECK(text & PTE_P); CHECK(!(text & PTE_W)); CHECK(!(text & PTE_NX));
    CHECK(ro & PTE_P);   CHECK(!(ro & PTE_W));   CHECK(ro & PTE_NX);
    CHECK(data & PTE_W); CHECK(data & PTE_NX);
    uint64_t pa, fl;
    CHECK(pt_translate(&c, 0xFFFFFFFF80107000ull, &pa, &fl) == PT_NOT_MAPPED);
}
TEST(vm_build_without_nx) {
    enum pt_status st;
    struct pt_ctx c = build(0, &st);
    CHECK(st == PT_OK);
    CHECK(!(flags_at(&c, PHYSMAP_BASE, 0) & PTE_NX));
    CHECK(!(flags_at(&c, 0xFFFFFFFF80102000ull, 0x102000) & PTE_NX));
}
TEST(vm_build_rejects_shared_pages) {
    nucleus.seg[1].vaddr = 0xFFFFFFFF80100800ull;          /* rodata in the text page */
    nucleus.seg[1].paddr = 0x100800;
    enum pt_status st;
    build(1, &st);
    CHECK(st == PT_ALREADY_MAPPED);
    nucleus.seg[1].vaddr = 0xFFFFFFFF80101000ull;
    nucleus.seg[1].paddr = 0x101000;
}
