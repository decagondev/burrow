/* sigtest.c - signals end to end (W10-S04): a handler for our own signal,
 * one that interrupts a child computing in a loop (no system calls), a
 * default action killing a busy child, EINTR on a blocked read, SIGCHLD. */
#include <errno.h>
#include <signal.h>
#include <stdio.h>
#include <string.h>
#include <sys/wait.h>
#include <unistd.h>

static volatile sig_atomic_t got, children;

static void on_signal(int sig) { got = sig; }
static void on_child(int sig) { (void)sig; children++; }
static void on_int(int sig) { (void)sig; _exit(3); }

static void handle(int sig, void (*fn)(int)) {
    struct sigaction sa;
    memset(&sa, 0, sizeof sa);
    sa.sa_handler = fn;          /* no SA_RESTART: blocked calls fail with EINTR */
    sigaction(sig, &sa, 0);
}

static void report(const char *what, pid_t pid) {
    int st;
    waitpid(pid, &st, 0);
    if (WIFSIGNALED(st)) printf("sigtest: %s killed by signal %d\n", what, WTERMSIG(st));
    else printf("sigtest: %s exited with %d\n", what, WEXITSTATUS(st));
}

int main(void) {
    handle(SIGUSR1, on_signal);
    handle(SIGCHLD, on_child);
    kill(getpid(), SIGUSR1);
    printf("sigtest: handler saw signal %d\n", (int)got);

    pid_t busy = fork();
    if (busy == 0) {
        handle(SIGINT, on_int);
        for (volatile unsigned long i = 0;; i++) { }   /* no system calls */
    }
    usleep(20000);
    kill(busy, SIGINT);
    report("busy child with a SIGINT handler", busy);

    busy = fork();
    if (busy == 0) for (volatile unsigned long i = 0;; i++) { }
    usleep(20000);
    kill(busy, SIGTERM);
    report("busy child", busy);

    int fds[2];
    pipe(fds);
    pid_t poker = fork();
    if (poker == 0) {
        usleep(20000);
        kill(getppid(), SIGUSR1);
        _exit(0);
    }
    char c;
    ssize_t n = read(fds[0], &c, 1);
    printf("sigtest: read returned %d (%s)\n", (int)n, n < 0 && errno == EINTR ? "EINTR" : "?");
    waitpid(poker, 0, 0);
    printf("sigtest: SIGCHLD seen %s\n", children >= 3 ? "for every child" : "too few times");
    return 0;
}
