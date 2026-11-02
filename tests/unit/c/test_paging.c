/* Builds page tables in a static pool with FAKE physical addresses, then walks
 * them like the MMU would. Fake addresses prove the builder never confuses a
 * physical address with a pointer. */
#include <stdlib.h>
#include "minitest.h"
#include "paging.h"

#define POOL_FRAMES 16
#define FAKE_PHYS   0x40000000ull     /* "physical" address of pool[0] */

static unsigned char pool[POOL_FRAMES][4096] __attribute__((aligned(4096)));
static unsigned used, budget;

static uint64_t fake_alloc(void *env) {
    (void)env;
    if (used == budget) return 0;
    memset(pool[used], 0xA5, sizeof pool[used]);   /* garbage: builder must zero it */
    return FAKE_PHYS + 4096ull * used++;
}
static void *fake_ptr(void *env, uint64_t pa) {
    (void)env;
    if (pa < FAKE_PHYS || pa >= FAKE_PHYS + sizeof pool) abort();  /* followed a bad pointer */
    return pool[(pa - FAKE_PHYS) / 4096];
}
static struct pt_ctx fresh(unsigned frames) {
    struct pt_ctx c = { fake_alloc, fake_ptr, 0, 0 };
    used = 0; budget = frames;
    if (pt_init(&c) != PT_OK) abort();
    return c;
}

TEST(paging_maps_4k_pages) {
    struct pt_ctx c = fresh(POOL_FRAMES);
    CHECK(pt_map(&c, 0xFFFFFFFF80100000ull, 0x100000, 0x3000, PTE_W | PTE_NX) == PT_OK);
    uint64_t pa, fl;
    CHECK(pt_translate(&c, 0xFFFFFFFF80101234ull, &pa, &fl) == PT_OK);
    CHECK_EQ_U64(pa, 0x101234);
    CHECK_EQ_U64(fl, PTE_P | PTE_W | PTE_NX);
    CHECK(pt_translate(&c, 0xFFFFFFFF80103000ull, &pa, &fl) == PT_NOT_MAPPED);
    CHECK_EQ_U64(used, 4);                          /* PML4 + PDPT + PD + PT */
}
TEST(paging_maps_2m_pages) {
    struct pt_ctx c = fresh(POOL_FRAMES);
    CHECK(pt_map(&c, 0, 0, 1ull << 30, PTE_W) == PT_OK);          /* identity 1 GiB */
    uint64_t pa, fl;
    CHECK(pt_translate(&c, 0x12345678, &pa, &fl) == PT_OK);
    CHECK_EQ_U64(pa, 0x12345678);
    CHECK_EQ_U64(fl, PTE_P | PTE_W | PTE_PS);
    CHECK_EQ_U64(used, 3);                          /* no PTs needed */
}
TEST(paging_mixes_page_sizes) {
    struct pt_ctx c = fresh(POOL_FRAMES);
    uint64_t base = 0xFFFF800000000000ull;
    CHECK(pt_map(&c, base, 0x200000, PAGE_2M + PAGE_4K, 0) == PT_OK);
    uint64_t pa, fl;
    CHECK(pt_translate(&c, base + 0x1FFFFF, &pa, &fl) == PT_OK);
    CHECK_EQ_U64(pa, 0x3FFFFF); CHECK(fl & PTE_PS);
    CHECK(pt_translate(&c, base + PAGE_2M + 8, &pa, &fl) == PT_OK);
    CHECK_EQ_U64(pa, 0x400008); CHECK(!(fl & PTE_PS)); CHECK(!(fl & PTE_W));
    /* pa not 2 MiB aligned -> must fall back to 4 KiB pages */
    CHECK(pt_map(&c, 0x40000000, 0x1000, PAGE_2M, 0) == PT_OK);
    CHECK(pt_translate(&c, 0x40000000, &pa, &fl) == PT_OK); CHECK(!(fl & PTE_PS));
}
TEST(paging_user_bit_reaches_every_level) {
    struct pt_ctx c = fresh(POOL_FRAMES);
    CHECK(pt_map(&c, 0x400000, 0x800000, PAGE_4K, PTE_U | PTE_W) == PT_OK);
    uint64_t *t = fake_ptr(0, c.root);
    for (unsigned level = 3; level > 0; level--) {
        uint64_t e = t[(0x400000ull >> (12 + 9 * level)) & 511];
        CHECK(e & PTE_U);
        t = fake_ptr(0, e & PTE_ADDR_MASK);
    }
}
TEST(paging_rejects_bad_input) {
    struct pt_ctx c = fresh(POOL_FRAMES);
    CHECK(pt_map(&c, 0x1001, 0, PAGE_4K, 0) == PT_UNALIGNED);
    CHECK(pt_map(&c, 0x1000, 0x10, PAGE_4K, 0) == PT_UNALIGNED);
    CHECK(pt_map(&c, 0x1000, 0, 0, 0) == PT_UNALIGNED);
    CHECK(pt_map(&c, 0x0000800000000000ull, 0, PAGE_4K, 0) == PT_NON_CANONICAL);
    CHECK(pt_map(&c, 0x00007FFFFFFFF000ull, 0, 2 * PAGE_4K, 0) == PT_NON_CANONICAL);
    CHECK(pt_map(&c, 0xFFFFFFFFFFFFF000ull, 0, 2 * PAGE_4K, 0) == PT_NON_CANONICAL);
    CHECK(pt_map(&c, 0x1000, 1ull << 52, PAGE_4K, 0) == PT_BAD_PHYS);
    uint64_t pa, fl;
    CHECK(pt_translate(&c, 0x0000900000000000ull, &pa, &fl) == PT_NON_CANONICAL);
}
TEST(paging_refuses_to_overwrite) {
    struct pt_ctx c = fresh(POOL_FRAMES);
    CHECK(pt_map(&c, 0x1000, 0x1000, PAGE_4K, 0) == PT_OK);
    CHECK(pt_map(&c, 0x1000, 0x5000, PAGE_4K, 0) == PT_ALREADY_MAPPED);
    CHECK(pt_map(&c, 0x200000, 0x200000, PAGE_2M, 0) == PT_OK);
    CHECK(pt_map(&c, 0x201000, 0x9000, PAGE_4K, 0) == PT_ALREADY_MAPPED);   /* inside a 2 MiB page */
    CHECK(pt_map(&c, 0, 0, PAGE_2M, 0) == PT_ALREADY_MAPPED);               /* PD slot holds a PT */
}
TEST(paging_out_of_memory) {
    struct pt_ctx c = fresh(2);                     /* PML4 + PDPT only */
    CHECK(pt_map(&c, 0x1000, 0x1000, PAGE_4K, 0) == PT_NO_MEMORY);
    CHECK_STR(pt_status_str(PT_NO_MEMORY), "out of memory");
}
