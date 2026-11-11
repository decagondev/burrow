/* lat.c - what a round trip costs (W25-S01): getpid, a clock read, and
 * ping-pongs through a pipe and a socketpair between two processes, a
 * futex between two threads and loopback TCP between two processes, each
 * timed in batches and reported as the median batch's time per operation:
 * "lat: <name> <us> us". Runs the same on the host's Linux (make lat-linux)
 * and on Burrow (make lat-bench). When /dev/lattrace exists (Burrow), it
 * also traces getpid's requests and prints the nucleus's steps:
 * "lat: step <name> <us> us". Never changes anything but its own pipes,
 * sockets and threads. Docs: docs/04-plan/waves/WAVE-25-speed.md. */
#include <arpa/inet.h>
#include <fcntl.h>
#include <linux/futex.h>
#include <netinet/in.h>
#include <netinet/tcp.h>
#include <pthread.h>
#include <stdatomic.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <sys/socket.h>
#include <sys/syscall.h>
#include <sys/wait.h>
#include <time.h>
#include <unistd.h>

#define BATCHES   7
#define CALLS     500   /* per batch, for the calls */
#define TRIPS     100   /* per batch, for the ping-pongs */
#define TRACED    1000  /* getpid requests traced through /dev/lattrace */

static double now_us(void) {
    struct timespec ts;
    clock_gettime(CLOCK_MONOTONIC, &ts);
    return ts.tv_sec * 1e6 + ts.tv_nsec / 1e3;
}

static int cmp(const void *a, const void *b) {
    double x = *(const double *)a, y = *(const double *)b;
    return (x > y) - (x < y);
}

/* The median of BATCHES runs of fn(n), in microseconds per operation. */
static double median(void (*fn)(int), int n) {
    double t[BATCHES];
    for (int i = 0; i < BATCHES; i++) {
        double t0 = now_us();
        fn(n);
        t[i] = (now_us() - t0) / n;
    }
    qsort(t, BATCHES, sizeof t[0], cmp);
    return t[BATCHES / 2];
}

static void do_getpid(int n) { for (int i = 0; i < n; i++) syscall(SYS_getpid); }
static void do_clock(int n) { struct timespec ts; for (int i = 0; i < n; i++) clock_gettime(CLOCK_MONOTONIC, &ts); }

/* A ping-pong over two descriptors: write one byte, read one back. */
static int ping_out = -1, ping_in = -1;
static void do_ping(int n) {
    char c = 'x';
    for (int i = 0; i < n; i++) {
        if (write(ping_out, &c, 1) != 1 || read(ping_in, &c, 1) != 1) { perror("lat: ping"); exit(1); }
    }
}

/* The other end echoes every byte until its input closes; it closes the
 * parent's descriptors (mine, mine2; -1 for none) so that the parent's
 * close is the end of its input. */
static pid_t echo_child(int in, int out, int mine, int mine2) {
    pid_t pid = fork();
    if (pid == 0) {
        if (mine >= 0) close(mine);
        if (mine2 >= 0) close(mine2);
        char c;
        while (read(in, &c, 1) == 1) if (write(out, &c, 1) != 1) break;
        _exit(0);
    }
    return pid;
}

static double ping_pipes(void) {
    int a[2], b[2];
    if (pipe(a) || pipe(b)) { perror("lat: pipe"); exit(1); }
    pid_t pid = echo_child(a[0], b[1], a[1], b[0]);
    close(a[0]); close(b[1]);
    ping_out = a[1]; ping_in = b[0];
    double us = median(do_ping, TRIPS);
    close(a[1]); close(b[0]); waitpid(pid, 0, 0);
    return us;
}

static double ping_socketpair(void) {
    int sv[2];
    if (socketpair(AF_UNIX, SOCK_STREAM, 0, sv)) { perror("lat: socketpair"); exit(1); }
    pid_t pid = echo_child(sv[1], sv[1], sv[0], -1);
    close(sv[1]);
    ping_out = ping_in = sv[0];
    double us = median(do_ping, TRIPS);
    close(sv[0]); waitpid(pid, 0, 0);
    return us;
}

static double ping_tcp(void) {
    int l = socket(AF_INET, SOCK_STREAM, 0), one = 1;
    struct sockaddr_in sa = { .sin_family = AF_INET, .sin_addr.s_addr = htonl(INADDR_LOOPBACK) };
    socklen_t len = sizeof sa;
    if (l < 0 || bind(l, (struct sockaddr *)&sa, sizeof sa) || listen(l, 1) || getsockname(l, (struct sockaddr *)&sa, &len)) {
        perror("lat: tcp listen"); exit(1);
    }
    int c = socket(AF_INET, SOCK_STREAM, 0);
    if (c < 0 || connect(c, (struct sockaddr *)&sa, sizeof sa)) { perror("lat: tcp connect"); exit(1); }
    int s = accept(l, 0, 0);
    if (s < 0) { perror("lat: tcp accept"); exit(1); }
    setsockopt(c, IPPROTO_TCP, TCP_NODELAY, &one, sizeof one);
    setsockopt(s, IPPROTO_TCP, TCP_NODELAY, &one, sizeof one);
    pid_t pid = echo_child(s, s, c, l);
    close(s); close(l);
    ping_out = ping_in = c;
    double us = median(do_ping, TRIPS);
    close(c); waitpid(pid, 0, 0);
    return us;
}

/* Two threads take turns through a futex word: MINE is the main thread's
 * turn, ECHO the echo thread's, QUIT ends it. */
enum { MINE, ECHO, QUIT };
static atomic_int turn;
static void futex_wait(int v) { syscall(SYS_futex, &turn, FUTEX_WAIT_PRIVATE, v, 0, 0, 0); }
static void futex_wake(void) { syscall(SYS_futex, &turn, FUTEX_WAKE_PRIVATE, 1, 0, 0, 0); }
static void *futex_echo(void *arg) {
    (void)arg;
    for (;;) {
        int v;
        while ((v = atomic_load(&turn)) == MINE) futex_wait(MINE);
        if (v == QUIT) return 0;
        atomic_store(&turn, MINE);
        futex_wake();
    }
}
static void do_futex(int n) {
    for (int i = 0; i < n; i++) {
        atomic_store(&turn, ECHO);
        futex_wake();
        while (atomic_load(&turn) == ECHO) futex_wait(ECHO);
    }
}
static double ping_futex(void) {
    pthread_t th;
    atomic_store(&turn, MINE);
    if (pthread_create(&th, 0, futex_echo, 0)) { perror("lat: pthread_create"); exit(1); }
    double us = median(do_futex, TRIPS);
    atomic_store(&turn, QUIT);
    futex_wake();
    pthread_join(th, 0);
    return us;
}

/* The nucleus's steps of one call's round trips while fn runs (Burrow
 * only), printed as "lat: <label>step <name> <us> us" and "lat: <label>trace
 * <line>": getpid's (label "") and a pipe read's during a ping-pong
 * ("pipe-"), where the reader waits (W25-S03). */
static void trace(const char *label, int nr, void (*fn)(void)) {
    int fd = open("/dev/lattrace", O_RDWR);
    if (fd < 0) return;
    char word[16], buf[512];
    snprintf(word, sizeof word, "%d\n", nr);
    if (write(fd, word, strlen(word)) < 0) { close(fd); return; }
    fn();
    close(fd);
    fd = open("/dev/lattrace", O_RDWR);
    ssize_t n = fd < 0 ? -1 : read(fd, buf, sizeof buf - 1);
    if (fd >= 0) { if (write(fd, "off\n", 4) < 0) perror("lat: off"); close(fd); }
    if (n <= 0) return;
    buf[n] = 0;
    for (char *line = strtok(buf, "\n"); line; line = strtok(0, "\n")) {
        char name[32]; double us;
        size_t len = strlen(line);
        int is_step = len > 3 && strcmp(line + len - 3, " us") == 0;   /* not "go-call N x per request" */
        if (is_step && sscanf(line, "%31s %lf us", name, &us) == 2 && strchr(name, '-'))
            printf("lat: %sstep %s %.2f us\n", label, name, us);
        else printf("lat: %strace %s\n", label, line);
    }
}
static void traced_getpid(void) { do_getpid(TRACED); }
static void traced_pipes(void) { ping_pipes(); }

int main(void) {
    setvbuf(stdout, 0, _IOLBF, 0);
    printf("lat: median of %d batches\n", BATCHES);
    printf("lat: getpid %.2f us\n", median(do_getpid, CALLS));
    printf("lat: clock %.2f us\n", median(do_clock, CALLS));
    printf("lat: pipe %.2f us\n", ping_pipes());
    printf("lat: futex %.2f us\n", ping_futex());
    printf("lat: socketpair %.2f us\n", ping_socketpair());
    printf("lat: tcp %.2f us\n", ping_tcp());
    fflush(stdout);
    trace("", SYS_getpid, traced_getpid);
    trace("pipe-", SYS_read, traced_pipes);
    trace("pipew-", SYS_write, traced_pipes);   /* W25-S04: the call that wakes the reader */
    printf("lat: ok\n");
    return 0;
}
