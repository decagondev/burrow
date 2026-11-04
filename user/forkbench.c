/* forkbench.c - what fork costs (W18-S01): a process with 64 MiB touched
 * forks ROUNDS times (each child exits at once; the time is fork to
 * waitpid), then forks once more and, while that child waits on a pipe,
 * asks sysinfo how much memory the system lost to it, then lets the child
 * write 1 MiB and asks again. With copy-on-write the waiting child costs
 * page tables, not 64 MiB; its writes cost what they touch. Runs on the
 * host's Linux and on Burrow. Prints "forkbench: ..." lines and
 * "forkbench: ok". Never checks the numbers itself: make mem-bench does.
 * Docs: docs/04-plan/waves/WAVE-18-foundations.md. */
#define _GNU_SOURCE
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <sys/sysinfo.h>
#include <sys/wait.h>
#include <time.h>
#include <unistd.h>

enum { MIB = 1 << 20, SIZE = 64 * MIB, ROUNDS = 10, WRITTEN = 1 * MIB };

static double now_us(void) {
    struct timespec t;
    clock_gettime(CLOCK_MONOTONIC, &t);
    return t.tv_sec * 1e6 + t.tv_nsec / 1e3;
}

static long free_kib(void) {
    struct sysinfo si;
    sysinfo(&si);
    return (long)(si.freeram * si.mem_unit / 1024);
}

int main(void) {
    setvbuf(stdout, NULL, _IONBF, 0);
    char *mem = malloc(SIZE);
    if (!mem) { puts("forkbench: FAIL no memory"); return 1; }
    memset(mem, 7, SIZE); /* every page touched: a copying fork copies all of it */
    double total = 0;
    for (int i = 0; i < ROUNDS; i++) {
        double t = now_us();
        pid_t pid = fork();
        if (pid == 0) _exit(mem[i * 4096] == 7 ? 0 : 1);
        int status;
        waitpid(pid, &status, 0);
        total += now_us() - t;
        if (!WIFEXITED(status) || WEXITSTATUS(status) != 0) { puts("forkbench: FAIL child saw wrong bytes"); return 1; }
    }
    printf("forkbench: fork of a 64 MiB process: %.0f us (fork to waitpid, %d rounds)\n", total / ROUNDS, ROUNDS);

    int go[2], done[2];
    if (pipe(go) || pipe(done)) { puts("forkbench: FAIL pipe"); return 1; }
    long before = free_kib();
    pid_t pid = fork();
    if (pid == 0) {
        char c;
        read(go[0], &c, 1);
        memset(mem, 9, WRITTEN); /* the child's writes copy only these pages */
        write(done[1], "w", 1);
        read(go[0], &c, 1);
        _exit(0);
    }
    usleep(100000);
    long idle = before - free_kib();
    char c;
    write(go[1], "g", 1);
    read(done[0], &c, 1);
    long wrote = before - free_kib();
    write(go[1], "x", 1);
    waitpid(pid, NULL, 0);
    printf("forkbench: a waiting child costs %ld KiB; after writing 1 MiB, %ld KiB\n", idle, wrote);
    printf("forkbench: parent still sees its bytes: %s\n", mem[0] == 7 && mem[SIZE - 1] == 7 ? "yes" : "NO");
    puts(mem[0] == 7 ? "forkbench: ok" : "forkbench: FAIL");
    return mem[0] != 7;
}
