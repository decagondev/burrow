/* lattrace.c - see lattrace.h. Pure: the caller holds the lock that covers
 * the stats (uthread.c's) and takes the stamps. */
#include "lattrace.h"

void lat_start(struct lat_stats *s, uint64_t nr) {
    s->nr = nr;
    s->count = s->skipped = 0;
    for (int i = 0; i < LAT_STEPS; i++) s->sum[i] = 0;
    s->on = 1;
}

void lat_stop(struct lat_stats *s) { s->on = 0; }

int lat_wanted(const struct lat_stats *s, uint64_t nr) { return s->on && s->nr == nr; }

void lat_record(struct lat_stats *s, const uint64_t stamps[LAT_STAMPS], uint64_t out) {
    uint64_t t[LAT_STAMPS + 1];
    for (int i = 0; i < LAT_STAMPS; i++) t[i] = stamps[i];
    t[LAT_STAMPS] = out;
    for (int i = 0; i < LAT_STAMPS; i++) {
        if (t[i] == 0 || t[i + 1] < t[i]) { s->skipped++; return; }
    }
    for (int i = 0; i < LAT_STEPS; i++) s->sum[i] += t[i + 1] - t[i];
    s->count++;
}
