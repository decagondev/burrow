/* startup.c - the small calls start-up code makes (W14-S03): select and
 * pselect6, non-blocking pipes, getrandom and /dev/urandom, statx, readlink,
 * sysinfo, umask, getrusage, the scheduler queries and membarrier. Every
 * check holds on Linux too: `make ports-smoke` runs this binary on the host
 * before it boots Burrow, so the two answer alike. Prints "startup: <name>
 * ok" per check, then "startup: ok"; a failed check prints FAIL. Never
 * checks what differs between machines (RAM sizes, the caller's umask).
 * Docs: docs/04-plan/waves/WAVE-14-desktop-groundwork.md. */
#define _GNU_SOURCE
#include <errno.h>
#include <fcntl.h>
#include <sched.h>
#include <stdint.h>
#include <stdio.h>
#include <string.h>
#include <sys/random.h>
#include <sys/resource.h>
#include <sys/select.h>
#include <sys/stat.h>
#include <sys/syscall.h>
#include <sys/sysinfo.h>
#include <time.h>
#include <unistd.h>

#define STATX_BYTES 256
#define STATX_BASIC 0x7ffu
#define GRND_BAD (GRND_RANDOM | 4) /* GRND_RANDOM | GRND_INSECURE */

static int failures;

static void check(const char *name, int good) {
    printf("startup: %s %s\n", name, good ? "ok" : "FAIL");
    if (!good) failures++;
}

static long ms_now(void) {
    struct timespec t;
    clock_gettime(CLOCK_MONOTONIC, &t);
    return t.tv_sec * 1000 + t.tv_nsec / 1000000;
}

static void selects(void) {
    int p[2];
    pipe(p);
    fd_set r, w;
    struct timeval zero = {0, 0};
    FD_ZERO(&r); FD_ZERO(&w); FD_SET(p[0], &r); FD_SET(p[1], &w);
    int n = select(p[1] + 1, &r, &w, 0, &zero);
    check("select-empty-pipe", n == 1 && !FD_ISSET(p[0], &r) && FD_ISSET(p[1], &w));
    write(p[1], "x", 1);
    FD_ZERO(&r); FD_SET(p[0], &r);
    check("select-data", select(p[0] + 1, &r, 0, 0, 0) == 1 && FD_ISSET(p[0], &r));
    FD_ZERO(&r); FD_SET(p[0] + 20, &r);
    check("select-ebadf", select(p[0] + 21, &r, 0, 0, &zero) == -1 && errno == EBADF);
    struct timeval tv = {0, 20000};
    long t0 = ms_now();
    check("select-sleep", select(0, 0, 0, 0, &tv) == 0 && ms_now() - t0 >= 15);
    struct timespec ts = {0, 0};
    FD_ZERO(&r); FD_SET(p[0], &r);
    check("pselect", pselect(p[0] + 1, &r, 0, 0, &ts, 0) == 1 && FD_ISSET(p[0], &r));
    close(p[0]); close(p[1]);
}

static void pipes_and_random(void) {
    int p[2];
    char c;
    check("pipe2-nonblock", pipe2(p, O_NONBLOCK) == 0 && (fcntl(p[0], F_GETFL) & O_NONBLOCK) &&
                                read(p[0], &c, 1) == -1 && errno == EAGAIN);
    close(p[0]); close(p[1]);
    unsigned char a[32], b[32];
    check("getrandom", getrandom(a, sizeof a, 0) == sizeof a && getrandom(b, sizeof b, GRND_NONBLOCK) == sizeof b &&
                           memcmp(a, b, sizeof a) != 0);
    check("getrandom-einval", getrandom(a, 8, GRND_BAD) == -1 && errno == EINVAL);
    int fd = open("/dev/urandom", O_RDONLY);
    check("dev-urandom", fd >= 0 && read(fd, a, 16) == 16);
    close(fd);
}

static uint32_t u32(const unsigned char *b, int off) { uint32_t v; memcpy(&v, b + off, 4); return v; }
static uint64_t u64(const unsigned char *b, int off) { uint64_t v; memcpy(&v, b + off, 8); return v; }

static void files(const char *self) {
    unsigned char x[STATX_BYTES];
    struct stat st;
    stat(self, &st);
    long r = syscall(SYS_statx, AT_FDCWD, self, 0, STATX_BASIC, x);
    check("statx", r == 0 && (u32(x, 0) & STATX_BASIC) == STATX_BASIC && u64(x, 40) == (uint64_t)st.st_size &&
                       (u32(x, 28) & 0xFFFF) == (st.st_mode & 0xFFFF));
    check("statx-enoent", syscall(SYS_statx, AT_FDCWD, "/no/such/file", 0, STATX_BASIC, x) == -1 && errno == ENOENT);
    char buf[64];
    check("readlink-file", readlink(self, buf, sizeof buf) == -1 && errno == EINVAL);
    check("readlink-enoent", readlink("/no/such/file", buf, sizeof buf) == -1 && errno == ENOENT);
}

static void system_calls(void) {
    struct sysinfo si;
    check("sysinfo", sysinfo(&si) == 0 && si.mem_unit >= 1 && si.totalram > 0 && si.freeram <= si.totalram &&
                         si.procs >= 1);
    mode_t old = umask(077);
    check("umask", umask(old) == 077);
    struct rusage ru;
    check("getrusage", getrusage(RUSAGE_SELF, &ru) == 0);
    check("getrusage-einval", syscall(SYS_getrusage, 5, &ru) == -1 && errno == EINVAL);
    struct sched_param sp = {.sched_priority = 7};
    check("sched_getparam", syscall(SYS_sched_getparam, 0, &sp) == 0 && sp.sched_priority == 0);
    check("sched_getscheduler", syscall(SYS_sched_getscheduler, 0) == SCHED_OTHER);
    cpu_set_t set;
    CPU_ZERO(&set);
    CPU_SET(0, &set);
    check("sched_setaffinity", sched_setaffinity(0, sizeof set, &set) == 0);
    CPU_ZERO(&set);
    check("sched_getaffinity", sched_getaffinity(0, sizeof set, &set) == 0 && CPU_ISSET(0, &set));
    long m = syscall(SYS_membarrier, 0, 0); /* MEMBARRIER_CMD_QUERY */
    check("membarrier", m >= 0 || errno == ENOSYS);
}

int main(int argc, char **argv) {
    (void)argc;
    setvbuf(stdout, 0, _IONBF, 0);
    selects();
    pipes_and_random();
    files(argv[0]);
    system_calls();
    if (failures) {
        printf("startup: %d FAILED\n", failures);
        return 1;
    }
    printf("startup: ok\n");
    return 0;
}
