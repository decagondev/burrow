/* Synthetic E820 maps: holes, overlapping reserved entries, exhaustion. */
#include "minitest.h"
#include "palloc.h"

#define MiB 0x100000ull
#define KiB 0x400ull

static const struct e820_entry simple[] = {
    { 0x0,     0x9FC00,  E820_USABLE,   1 },
    { 0x9FC00, 0x400,    E820_RESERVED, 1 },
    { 1 * MiB, 15 * MiB, E820_USABLE,   1 },
};

TEST(palloc_bumps_from_floor) {
    struct palloc p;
    palloc_init(&p, simple, 3, 1 * MiB, 1024 * MiB);
    CHECK_EQ_U64(palloc_alloc(&p, 1), 1 * MiB);
    CHECK_EQ_U64(palloc_alloc(&p, 2), 1 * MiB + 4 * KiB);
    CHECK_EQ_U64(palloc_alloc(&p, 1), 1 * MiB + 12 * KiB);
    CHECK_EQ_U64(p.first, 1 * MiB);
    CHECK_EQ_U64(p.end, 1 * MiB + 16 * KiB);
}
TEST(palloc_skips_reserved_ranges) {
    struct palloc p;
    palloc_init(&p, simple, 3, 1 * MiB, 1024 * MiB);
    CHECK(palloc_reserve(&p, 1 * MiB, 3 * MiB) == 0);         /* nucleus window */
    CHECK(palloc_reserve(&p, 4 * MiB + 100, 1) == 0);         /* unaligned, 1 byte */
    CHECK_EQ_U64(palloc_alloc(&p, 1), 4 * MiB + 4 * KiB);
}
TEST(palloc_jumps_holes) {
    static const struct e820_entry holes[] = {
        { 1 * MiB, 8 * KiB, E820_USABLE, 1 },
        { 3 * MiB, 1 * MiB, E820_USABLE, 1 },
    };
    struct palloc p;
    palloc_init(&p, holes, 2, 1 * MiB, 1024 * MiB);
    CHECK_EQ_U64(palloc_alloc(&p, 1), 1 * MiB);
    CHECK_EQ_U64(palloc_alloc(&p, 2), 3 * MiB);   /* only 1 frame left in the first entry */
    CHECK_EQ_U64(palloc_alloc(&p, 1), 3 * MiB + 8 * KiB);
}
TEST(palloc_avoids_overlapping_nonusable) {
    static const struct e820_entry odd[] = {
        { 1 * MiB, 15 * MiB, E820_USABLE,   1 },
        { 1 * MiB, 4 * KiB,  E820_RESERVED, 1 },   /* BIOS says both: reserved wins */
        { 0x100100, 0x10,    E820_BAD,      1 },
    };
    struct palloc p;
    palloc_init(&p, odd, 3, 1 * MiB, 1024 * MiB);
    CHECK_EQ_U64(palloc_alloc(&p, 1), 1 * MiB + 4 * KiB);
}
TEST(palloc_aligns_unaligned_entries) {
    static const struct e820_entry unaligned[] = { { 0x100123, 64 * KiB, E820_USABLE, 1 } };
    struct palloc p;
    palloc_init(&p, unaligned, 1, 1 * MiB, 1024 * MiB);
    CHECK_EQ_U64(palloc_alloc(&p, 1), 0x101000);
}
TEST(palloc_exhaustion_and_limit) {
    static const struct e820_entry tiny[] = { { 1 * MiB, 8 * KiB, E820_USABLE, 1 } };
    struct palloc p;
    palloc_init(&p, tiny, 1, 1 * MiB, 1024 * MiB);
    CHECK(palloc_alloc(&p, 1) != 0);
    CHECK(palloc_alloc(&p, 1) != 0);
    CHECK_EQ_U64(palloc_alloc(&p, 1), 0);
    CHECK_EQ_U64(palloc_alloc(&p, 0), 0);

    palloc_init(&p, simple, 3, 1 * MiB, 1 * MiB + 8 * KiB);    /* limit below RAM end */
    CHECK(palloc_alloc(&p, 2) != 0);
    CHECK_EQ_U64(palloc_alloc(&p, 1), 0);
    CHECK_EQ_U64(palloc_alloc(&p, 1ull << 60), 0);              /* no overflow */
}
TEST(palloc_reserved_table_full) {
    struct palloc p;
    palloc_init(&p, simple, 3, 1 * MiB, 1024 * MiB);
    for (unsigned i = 0; i < PALLOC_MAX_RESERVED; i++) CHECK(palloc_reserve(&p, 0, 1) == 0);
    CHECK(palloc_reserve(&p, 0, 1) == -1);
}
