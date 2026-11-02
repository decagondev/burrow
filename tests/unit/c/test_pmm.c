/* PMM free list over 32 fake frames at a fake physical address. */
#include <stdlib.h>
#include "minitest.h"
#include "pmm.h"

#define FRAMES    32
#define FAKE_PHYS 0x200000ull
static unsigned char ram[FRAMES][4096] __attribute__((aligned(4096)));

static void *ram_ptr(void *env, uint64_t pa) {
    (void)env;
    if (pa < FAKE_PHYS || pa >= FAKE_PHYS + sizeof ram || (pa & 0xFFF)) abort();
    return ram[(pa - FAKE_PHYS) / 4096];
}

static const struct e820_entry map[] = {
    { FAKE_PHYS + 0x10,  FRAMES * 4096ull - 0x10, E820_USABLE,   1 },  /* unaligned start: frame 0 lost */
    { FAKE_PHYS + 0x8000, 0x1000,                 E820_RESERVED, 1 },  /* frame 8 is firmware's */
};

static struct pmm fresh(const struct pmm_range *res, uint32_t nres) {
    struct pmm p;
    pmm_init(&p, ram_ptr, 0);
    pmm_add_e820(&p, map, 2, res, nres);
    return p;
}

TEST(pmm_counts_usable_frames) {
    struct pmm p = fresh(0, 0);
    CHECK_EQ_U64(p.total_frames, FRAMES - 2);       /* minus partial frame 0 and reserved frame 8 */
    CHECK_EQ_U64(p.free_frames, FRAMES - 2);
}
TEST(pmm_never_hands_out_reserved) {
    struct pmm_range res[] = { { FAKE_PHYS + 0x3000, 0x2000 } };   /* frames 3, 4 */
    struct pmm p = fresh(res, 1);
    CHECK_EQ_U64(p.total_frames, FRAMES - 4);
    uint64_t pa;
    while ((pa = pmm_alloc(&p)) != 0) {
        CHECK(pa >= FAKE_PHYS + 0x1000);
        CHECK(pa != FAKE_PHYS + 0x3000 && pa != FAKE_PHYS + 0x4000 && pa != FAKE_PHYS + 0x8000);
        CHECK((pa & 0xFFF) == 0);
    }
    CHECK_EQ_U64(p.free_frames, 0);
}
TEST(pmm_alloc_all_free_all) {
    struct pmm p = fresh(0, 0);
    static uint64_t got[FRAMES];
    unsigned n = 0;
    uint64_t pa;
    while ((pa = pmm_alloc(&p)) != 0) {
        for (unsigned i = 0; i < n; i++) CHECK(got[i] != pa);    /* no frame twice */
        got[n++] = pa;
    }
    CHECK_EQ_U64(n, FRAMES - 2);
    CHECK_EQ_U64(pmm_alloc(&p), 0);
    for (unsigned i = 0; i < n; i++) pmm_free(&p, got[i]);
    CHECK_EQ_U64(p.free_frames, FRAMES - 2);
    CHECK(pmm_alloc(&p) == got[n - 1]);              /* LIFO */
}
static void *ram_at_zero(void *env, uint64_t pa) {   /* same buffer, pretend it starts at phys 0 */
    (void)env;
    if (pa >= sizeof ram || (pa & 0xFFF)) abort();
    return ram[pa / 4096];
}
TEST(pmm_skips_frame_zero) {
    static const struct e820_entry low[] = { { 0, 0x3000, E820_USABLE, 1 } };
    struct pmm p;
    pmm_init(&p, ram_at_zero, 0);
    pmm_add_e820(&p, low, 1, 0, 0);
    CHECK_EQ_U64(p.total_frames, 2);                 /* 0 is the "empty" sentinel */
    CHECK(pmm_alloc(&p) != 0 && pmm_alloc(&p) != 0);
    CHECK_EQ_U64(pmm_alloc(&p), 0);
}
