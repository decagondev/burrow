/* proctest.c - /proc as the tools read it (W23-S01): /proc/self and its
 * stat (52 fields), status, cmdline, environ, comm and statm; a parent's and
 * a zombie child's entries and a reaped one's absence; the listing; uptime,
 * loadavg, meminfo, cpuinfo, version and sys/kernel against the system calls
 * that say the same; CPU time (W23-S02) from times, getrusage, wait4's rusage,
 * /proc/self/stat and /proc/stat against a busy loop. Every check holds on Linux too: `make ports-smoke` runs
 * this binary on the host first. Run it with arguments to check cmdline.
 * Prints "proctest: <name> ok" per check, then "proctest: ok"; a failed check
 * prints FAIL. Never checks numbers that differ between machines.
 * Docs: docs/04-plan/waves/WAVE-23-proc-virtio.md. */
#define _GNU_SOURCE
#include <dirent.h>
#include <errno.h>
#include <fcntl.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <sys/resource.h>
#include <sys/stat.h>
#include <sys/sysinfo.h>
#include <sys/times.h>
#include <sys/utsname.h>
#include <sys/wait.h>
#include <time.h>
#include <unistd.h>

enum { BUF = 1 << 20, STAT_FIELDS = 52 }; /* the host's cpuinfo and environment can be long */

static int failures;
static char buf[BUF];

static void check(const char *name, int good) {
    printf("proctest: %s %s\n", name, good ? "ok" : "FAIL");
    if (!good) failures++;
}

/* slurp reads a whole file into buf (NUL-terminated); its length or -1. */
static int slurp(const char *path) {
    int fd = open(path, O_RDONLY);
    if (fd < 0) return -1;
    int n = 0, k;
    while (n < BUF - 1 && (k = read(fd, buf + n, 7)) > 0) n += k; /* small reads: one snapshot */
    close(fd);
    buf[n] = 0;
    return n;
}

/* statFields counts a stat line's fields: "pid (comm)" then the rest. */
static int statFields(const char *line, char *state) {
    const char *p = strrchr(line, ')');
    if (!p) return 0;
    *state = p[2];
    int n = 2;
    for (p += 1; *p && *p != '\n'; p++)
        if (*p == ' ' && p[1] && p[1] != ' ' && p[1] != '\n') n++;
    return n;
}

static void self(int argc, char **argv, char **envp) {
    char want[32], link[32] = {0};
    snprintf(want, sizeof want, "%d", getpid());
    check("self-link", readlink("/proc/self", link, sizeof link - 1) > 0 && !strcmp(link, want));
    char state = 0;
    int pid = 0, ppid = 0;
    check("stat-read", slurp("/proc/self/stat") > 0);
    check("stat-fields", statFields(buf, &state) == STAT_FIELDS);
    check("stat-head", sscanf(buf, "%d (proctest) %*c %d", &pid, &ppid) == 2 && pid == getpid() && ppid == getppid() && state == 'R');
    slurp("/proc/self/status");
    char uid[64];
    snprintf(uid, sizeof uid, "\nUid:\t%d\t%d\t", getuid(), geteuid());
    check("status", !strncmp(buf, "Name:\tproctest\n", 15) && strstr(buf, "\nState:\tR (running)\n") &&
                        strstr(buf, uid) && strstr(buf, "\nThreads:\t1\n") && strstr(buf, " kB\n"));
    int n = slurp("/proc/self/cmdline"), at = 0, same = 1;
    for (int i = 0; i < argc; i++) {
        same = same && at < n && !strcmp(buf + at, argv[i]);
        at += strlen(argv[i]) + 1;
    }
    check("cmdline", same && at == n);
    n = slurp("/proc/self/environ"), at = 0, same = 1;
    for (char **e = envp; *e; e++) {
        same = same && at < n && !strcmp(buf + at, *e);
        at += strlen(*e) + 1;
    }
    check("environ", same && at == n);
    check("comm", slurp("/proc/self/comm") > 0 && !strcmp(buf, "proctest\n"));
    unsigned long size = 0, rss = 0;
    check("statm", slurp("/proc/self/statm") > 0 && sscanf(buf, "%lu %lu", &size, &rss) == 2 && size > 0 && rss <= size); /* Linux batches rss: may be 0 */
    struct stat st;
    check("owner", stat("/proc/self", &st) == 0 && S_ISDIR(st.st_mode) && st.st_uid == geteuid() &&
                       stat("/proc/self/environ", &st) == 0 && (st.st_mode & 0777) == 0400);
}

static char childState(int pid) {
    char path[64], state = 0;
    snprintf(path, sizeof path, "/proc/%d/stat", pid);
    if (slurp(path) < 0) return 0;
    statFields(buf, &state);
    return state;
}

static void others(void) {
    char path[64];
    snprintf(path, sizeof path, "/proc/%d/stat", getppid());
    int pid = 0;
    check("parent", slurp(path) > 0 && sscanf(buf, "%d", &pid) == 1 && pid == getppid());
    int child = fork();
    if (child == 0) _exit(3);
    char state = 0;
    for (int i = 0; i < 200 && (state = childState(child)) != 'Z'; i++) usleep(10000);
    check("zombie", state == 'Z');
    int status;
    waitpid(child, &status, 0);
    snprintf(path, sizeof path, "/proc/%d", child);
    struct stat st;
    check("reaped", stat(path, &st) == -1 && errno == ENOENT);
    DIR *d = opendir("/proc");
    int me = 0, selfSeen = 0;
    for (struct dirent *e; d && (e = readdir(d));) {
        me = me || atoi(e->d_name) == getpid();
        selfSeen = selfSeen || !strcmp(e->d_name, "self");
    }
    if (d) closedir(d);
    check("listing", me && selfSeen);
}

static void system_files(void) {
    double up = 0, idle = 0, l1, l5, l15;
    int running, threads, last;
    struct sysinfo si;
    sysinfo(&si);
    check("uptime", slurp("/proc/uptime") > 0 && sscanf(buf, "%lf %lf", &up, &idle) == 2 && up > 0 &&
                        up - si.uptime < 2 && si.uptime - up < 2);
    check("loadavg", slurp("/proc/loadavg") > 0 &&
                         sscanf(buf, "%lf %lf %lf %d/%d %d", &l1, &l5, &l15, &running, &threads, &last) == 6 &&
                         running >= 1 && threads >= running && last > 0);
    unsigned long total = 0, free = 0;
    check("meminfo", slurp("/proc/meminfo") > 0 && sscanf(buf, "MemTotal: %lu kB\nMemFree: %lu kB", &total, &free) == 2 &&
                         total >= free && free > 0 && strstr(buf, "\nMemAvailable:") && strstr(buf, "\nCached:"));
    slurp("/proc/cpuinfo");
    int cpus = 0;
    for (char *p = buf; (p = strstr(p, "processor\t: ")); p++) cpus++;
    check("cpuinfo", cpus == sysconf(_SC_NPROCESSORS_ONLN) && strstr(buf, "\nflags\t\t: "));
    struct utsname u;
    uname(&u);
    char want[200];
    snprintf(want, sizeof want, "%s version %s ", u.sysname, u.release);
    check("version", slurp("/proc/version") > 0 && !strncmp(buf, want, strlen(want)));
    snprintf(want, sizeof want, "%s\n", u.sysname);
    int ostype = slurp("/proc/sys/kernel/ostype") > 0 && !strcmp(buf, want);
    snprintf(want, sizeof want, "%s\n", u.release);
    int release = slurp("/proc/sys/kernel/osrelease") > 0 && !strcmp(buf, want);
    snprintf(want, sizeof want, "%s\n", u.nodename);
    check("sys-kernel", ostype && release && slurp("/proc/sys/kernel/hostname") > 0 && !strcmp(buf, want));
}

/* busy computes for ms milliseconds of wall time, making no system call
 * but the clock's (musl reads CLOCK_MONOTONIC through the vDSO or a call). */
static void busy(long ms) {
    struct timespec t0, t;
    clock_gettime(CLOCK_MONOTONIC, &t0);
    for (volatile unsigned long i = 0;; i++) {
        if (i % 100000) continue;
        clock_gettime(CLOCK_MONOTONIC, &t);
        if ((t.tv_sec - t0.tv_sec) * 1000 + (t.tv_nsec - t0.tv_nsec) / 1000000 >= ms) return;
    }
}

static long tv_ms(struct timeval tv) { return tv.tv_sec * 1000 + tv.tv_usec / 1000; }

/* statTicks is field n (1-based) of a stat line in buf. */
static unsigned long statTicks(int n) {
    const char *p = strrchr(buf, ')') + 2;
    for (int i = 3; i < n; i++) p = strchr(p, ' ') + 1;
    return strtoul(p, 0, 10);
}

static void cpu_times(void) {
    long hz = sysconf(_SC_CLK_TCK);
    busy(300);
    struct tms tm;
    check("clock-ticks", hz == 100);
    check("times", times(&tm) != (clock_t)-1 && tm.tms_utime >= 20 && tm.tms_utime <= 40 && tm.tms_stime < tm.tms_utime);
    struct rusage ru;
    getrusage(RUSAGE_SELF, &ru);
    check("getrusage", tv_ms(ru.ru_utime) >= 200 && tv_ms(ru.ru_utime) / 10 <= tm.tms_utime + 2 && ru.ru_maxrss > 0);
    slurp("/proc/self/stat");
    unsigned long ut = statTicks(14), st = statTicks(15);
    check("stat-times", ut + 2 >= (unsigned long)tm.tms_utime && ut <= (unsigned long)tm.tms_utime + 10 && st <= ut);
    int child = fork();
    if (child == 0) { busy(200); _exit(0); }
    int status;
    struct rusage cru;
    check("wait4-rusage", wait4(child, &status, 0, &cru) == child && tv_ms(cru.ru_utime) >= 120);
    times(&tm);
    getrusage(RUSAGE_CHILDREN, &ru);
    check("children", tm.tms_cutime >= 12 && tv_ms(ru.ru_utime) >= 120);
    unsigned long u0 = 0, u1 = 0, n0 = 0, s0 = 0, i0 = 0;
    check("proc-stat", slurp("/proc/stat") > 0 && sscanf(buf, "cpu %lu %lu %lu %lu", &u0, &n0, &s0, &i0) == 4 &&
                           strstr(buf, "\ncpu0 ") && strstr(buf, "\nctxt ") && strstr(buf, "\nbtime "));
    busy(100);
    slurp("/proc/stat");
    sscanf(buf, "cpu %lu", &u1);
    check("proc-stat-grows", u1 >= u0 + 5);
}

int main(int argc, char **argv, char **envp) {
    self(argc, argv, envp);
    others();
    system_files();
    cpu_times();
    printf("proctest: %s\n", failures ? "FAIL" : "ok");
    return failures != 0;
}
