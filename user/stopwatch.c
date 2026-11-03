/* stopwatch.c - "stopwatch prog [args]" sets QTBENCH_T0 to CLOCK_MONOTONIC
 * in nanoseconds and execs prog (W15-S05), so prog can tell how long its
 * own exec, load and start-up took (qtbench reads it). No fork: the clock
 * is read right before the execve. Docs: docs/04-plan/waves/WAVE-15-qt.md. */
#include <stdio.h>
#include <stdlib.h>
#include <time.h>
#include <unistd.h>

int main(int argc, char **argv) {
    if (argc < 2) {
        fprintf(stderr, "usage: stopwatch prog [args]\n");
        return 2;
    }
    struct timespec t;
    clock_gettime(CLOCK_MONOTONIC, &t);
    char ns[32];
    snprintf(ns, sizeof ns, "%lld", (long long)t.tv_sec * 1000000000LL + t.tv_nsec);
    setenv("QTBENCH_T0", ns, 1);
    execv(argv[1], argv + 1);
    perror("stopwatch: execv");
    return 127;
}
