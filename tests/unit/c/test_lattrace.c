/* test_lattrace.c - the round trip's step sums (W25-S01, nucleus/lattrace.c):
 * only the traced call number counts, a request's four steps add up, and a
 * request with a missing or backwards stamp is skipped, not summed. */
#include "minitest.h"
#include "lattrace.h"

TEST(lattrace_sums_the_steps) {
    struct lat_stats s;
    lat_start(&s, 39);
    CHECK(lat_wanted(&s, 39));
    CHECK(!lat_wanted(&s, 0));
    const uint64_t a[LAT_STAMPS] = { 100, 110, 160, 175 };
    lat_record(&s, a, 180);
    const uint64_t b[LAT_STAMPS] = { 1000, 1020, 1100, 1130 };
    lat_record(&s, b, 1131);
    CHECK_EQ_U64(s.count, 2);
    CHECK_EQ_U64(s.sum[0], 10 + 20);
    CHECK_EQ_U64(s.sum[1], 50 + 80);
    CHECK_EQ_U64(s.sum[2], 15 + 30);
    CHECK_EQ_U64(s.sum[3], 5 + 1);
    lat_stop(&s);
    CHECK(!lat_wanted(&s, 39));
}

TEST(lattrace_skips_bad_stamps) {
    struct lat_stats s;
    lat_start(&s, 0);
    const uint64_t missing[LAT_STAMPS] = { 100, 0, 160, 175 };
    lat_record(&s, missing, 180);
    const uint64_t backwards[LAT_STAMPS] = { 100, 90, 160, 175 };
    lat_record(&s, backwards, 180);
    const uint64_t late_out[LAT_STAMPS] = { 100, 110, 160, 175 };
    lat_record(&s, late_out, 170);
    CHECK_EQ_U64(s.count, 0);
    CHECK_EQ_U64(s.skipped, 3);
    CHECK_EQ_U64(s.sum[0], 0);
    lat_start(&s, 0);   /* a new start resets everything */
    CHECK_EQ_U64(s.skipped, 0);
}
