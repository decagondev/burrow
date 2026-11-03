/* syslat.c - how long a system call and a first-touch page fault take
 * (BUG-0014): 2000 getpid calls, then 2000 faults on fresh anonymous pages.
 * Prints both and "syslat: ok" when each averages under LIMIT_US; before the
 * fix every request waited about 2 ms for the dispatcher. A latency test,
 * not a benchmark: the limit is loose so slow CI machines pass.
 * Docs: docs/living/BUGS_AND_MITIGATIONS.md (BUG-0014). */
#include <stdio.h>
#include <sys/mman.h>
#include <sys/syscall.h>
#include <time.h>
#include <unistd.h>

#define ROUNDS 2000
#define PAGE 4096
#define LIMIT_US 500

static long us_now(void) {
    struct timespec t;
    clock_gettime(CLOCK_MONOTONIC, &t);
    return t.tv_sec * 1000000 + t.tv_nsec / 1000;
}

int main(void) {
    long t0 = us_now();
    for (int i = 0; i < ROUNDS; i++) syscall(SYS_getpid);
    long calls = (us_now() - t0) / ROUNDS;

    char *m = mmap(0, ROUNDS * PAGE, PROT_READ | PROT_WRITE, MAP_PRIVATE | MAP_ANONYMOUS, -1, 0);
    if (m == MAP_FAILED) { perror("syslat: mmap"); return 1; }
    t0 = us_now();
    for (int i = 0; i < ROUNDS; i++) m[i * PAGE] = 1;
    long faults = (us_now() - t0) / ROUNDS;

    printf("syslat: getpid %ld us, page fault %ld us (limit %d us)\n", calls, faults, LIMIT_US);
    if (calls > LIMIT_US || faults > LIMIT_US) { printf("syslat: FAIL too slow (BUG-0014)\n"); return 1; }
    printf("syslat: ok\n");
    return 0;
}
