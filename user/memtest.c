/* memtest.c - use a lot of memory (W14-S01). "memtest N" maps N MiB of
 * anonymous memory, writes every page with its own index (reporting the time
 * every 256 MiB), reads it all back and prints "memtest: N MiB ok".
 * "memtest oom" keeps mapping and touching 64 MiB chunks until the kernel
 * kills it for running out of memory; it never prints "ok". Never frees on
 * purpose: the kill must clean up.
 * Docs: docs/04-plan/waves/WAVE-14-desktop-groundwork.md. */
#include <stdint.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <sys/mman.h>
#include <time.h>

#define MIB (1024UL * 1024)
#define PAGE 4096UL
#define WORDS_PER_PAGE (PAGE / 8)
#define CHUNK_MIB 64
#define REPORT_MIB 256

static long ms_now(void) {
    struct timespec t;
    clock_gettime(CLOCK_MONOTONIC, &t);
    return t.tv_sec * 1000 + t.tv_nsec / 1000000;
}

static uint64_t *map(size_t bytes) {
    void *p = mmap(0, bytes, PROT_READ | PROT_WRITE, MAP_PRIVATE | MAP_ANONYMOUS, -1, 0);
    if (p == MAP_FAILED) { perror("memtest: mmap"); exit(1); }
    return p;
}

static void fill(uint64_t *mem, size_t pages, uint64_t base) {
    for (size_t i = 0; i < pages; i++) mem[i * WORDS_PER_PAGE] = base + i;
}

static size_t check(const uint64_t *mem, size_t pages, uint64_t base) {
    size_t bad = 0;
    for (size_t i = 0; i < pages; i++)
        if (mem[i * WORDS_PER_PAGE] != base + i || mem[i * WORDS_PER_PAGE + 1] != 0) bad++;
    return bad;
}

static int oom(void) {
    for (unsigned long mib = CHUNK_MIB;; mib += CHUNK_MIB) {
        uint64_t *mem = map(CHUNK_MIB * MIB);
        fill(mem, CHUNK_MIB * MIB / PAGE, mib);
        printf("memtest: touched %lu MiB\n", mib);
        fflush(stdout);
    }
}

int main(int argc, char **argv) {
    if (argc != 2) { fprintf(stderr, "usage: memtest <MiB> | memtest oom\n"); return 2; }
    if (!strcmp(argv[1], "oom")) return oom();
    unsigned long mib = strtoul(argv[1], 0, 10);
    size_t pages = mib * MIB / PAGE, step = REPORT_MIB * MIB / PAGE;
    uint64_t *mem = map(mib * MIB);
    long t0 = ms_now();
    for (size_t done = 0; done < pages; done += step) {
        size_t n = pages - done < step ? pages - done : step;
        fill(mem + done * WORDS_PER_PAGE, n, 0x5eed0000 + done);
        printf("memtest: %zu MiB touched in %ld ms\n", (done + n) * PAGE / MIB, ms_now() - t0);
        fflush(stdout);
    }
    size_t bad = check(mem, pages, 0x5eed0000);
    if (bad) { printf("memtest: FAIL %zu of %zu pages wrong\n", bad, pages); return 1; }
    munmap(mem, mib * MIB);
    printf("memtest: %lu MiB ok (%ld ms)\n", mib, ms_now() - t0);
    return 0;
}
