/* regions: hints, first fit, coverage across neighbours, release splits. */
#include "minitest.h"
#include "regions.h"

#define GiB 0x40000000ull
#define P   0x1000ull
#define LO  0x400000ull
#define HI  0x800000000000ull
#define BASE 0x7D0000000000ull

TEST(regions_hint_or_first_fit) {
    static struct regions rs;
    regions_init(&rs, LO, HI, BASE);
    uint64_t a, b, c;
    CHECK(regions_reserve(&rs, 0xC000000000ull, 64 * GiB, &a) == 0);
    CHECK_EQ_U64(a, 0xC000000000ull);                       /* hint honoured */
    CHECK(regions_reserve(&rs, 0xC000000000ull, P, &b) == 0);
    CHECK_EQ_U64(b, BASE);                                  /* hint taken: first fit */
    CHECK(regions_reserve(&rs, 0, 2 * P, &c) == 0);
    CHECK_EQ_U64(c, BASE + P);
    CHECK(regions_reserve(&rs, HI - P, 2 * P, &c) == 0);    /* hint crosses hi: ignored */
    CHECK_EQ_U64(c, BASE + 3 * P);
    CHECK(regions_reserve(&rs, 0, 0, &c) == -1);
}
TEST(regions_first_fit_uses_gaps) {
    static struct regions rs;
    regions_init(&rs, LO, HI, BASE);
    uint64_t a, b, c, d;
    CHECK(regions_reserve(&rs, 0, P, &a) == 0);
    CHECK(regions_reserve(&rs, 0, P, &b) == 0);
    CHECK(regions_reserve(&rs, 0, P, &c) == 0);
    CHECK(regions_release(&rs, b, P) == 0);
    CHECK(regions_reserve(&rs, 0, 2 * P, &d) == 0);         /* gap of 1 page is too small */
    CHECK_EQ_U64(d, c + P);
    CHECK(regions_reserve(&rs, 0, P, &d) == 0);             /* but fits one page */
    CHECK_EQ_U64(d, b);
}
TEST(regions_covers_neighbours) {
    static struct regions rs;
    regions_init(&rs, LO, HI, BASE);
    uint64_t a, b;
    CHECK(regions_reserve(&rs, 0, 2 * P, &a) == 0);
    CHECK(regions_reserve(&rs, 0, 2 * P, &b) == 0);         /* adjacent, not merged */
    CHECK(regions_covers(&rs, a + P, 2 * P));               /* spans both */
    CHECK(!regions_covers(&rs, a + P, 4 * P));
    CHECK(!regions_covers(&rs, a - P, 2 * P));
    CHECK(regions_covers(&rs, a, 4 * P));
}
TEST(regions_release_splits_and_trims) {
    static struct regions rs;
    regions_init(&rs, LO, HI, BASE);
    uint64_t a;
    CHECK(regions_reserve(&rs, 0, 10 * P, &a) == 0);
    CHECK(regions_release(&rs, a + 4 * P, 2 * P) == 0);     /* split */
    CHECK_EQ_U64(rs.count, 2);
    CHECK(regions_covers(&rs, a, 4 * P));
    CHECK(!regions_covers(&rs, a + 4 * P, P));
    CHECK(regions_covers(&rs, a + 6 * P, 4 * P));
    CHECK(regions_release(&rs, a, 5 * P) == 0);             /* whole head + nothing */
    CHECK_EQ_U64(rs.count, 1);
    CHECK(regions_release(&rs, a + 9 * P, 5 * P) == 0);     /* trims the tail */
    CHECK(regions_covers(&rs, a + 6 * P, 3 * P));
    CHECK(!regions_covers(&rs, a + 9 * P, P));
}
TEST(regions_full_table) {
    static struct regions rs;
    regions_init(&rs, LO, HI, BASE);
    uint64_t a;
    for (unsigned i = 0; i < REGIONS_MAX; i++) CHECK(regions_reserve(&rs, 0, 3 * P, &a) == 0);
    CHECK(regions_reserve(&rs, 0, P, &a) == -1);
    CHECK(regions_release(&rs, BASE, P) == 0);              /* trim a head: no new slot needed */
    CHECK(regions_release(&rs, BASE + 4 * P, P) == -1);     /* middle of region 2: split needs a slot */
}
