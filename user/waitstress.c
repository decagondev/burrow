/* waitstress.c - many children ending in many ways while their parent
 * waits (W20-S01, BUG-0023): exit at once, exit from main while threads
 * run (exit_group), exit from a thread, killed by a signal while asleep,
 * leaving an orphan behind, and exec of another program; the parent takes
 * SIGCHLD and reaps with waitpid(-1), checking every status. Each round
 * forks one child of each kind. Prints progress and "waitstress: N rounds
 * ok", or the round and child that went wrong. Docs: WAVE-20-smp.md. */
#include <errno.h>
#include <pthread.h>
#include <signal.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <sys/wait.h>
#include <time.h>
#include <unistd.h>

enum { EXIT, EXIT_WITH_THREADS, EXIT_FROM_THREAD, KILLED, ORPHAN, EXEC, KINDS };

static volatile sig_atomic_t chld;

static void on_chld(int sig) { (void)sig; chld++; }

static void *spin(void *arg) {
    volatile unsigned long n = 0;
    for (;;)
        n += (unsigned long)arg;
    return NULL;
}

static void *sleeper(void *arg) {
    (void)arg;
    for (;;)
        pause();
    return NULL;
}

static void *exiter(void *arg) {
    exit((int)(long)arg);
}

static void child(int kind, int code) {
    pthread_t t;
    switch (kind) {
    case EXIT:
        _exit(code);
    case EXIT_WITH_THREADS:
        pthread_create(&t, NULL, spin, (void *)1);
        pthread_create(&t, NULL, sleeper, NULL);
        pthread_create(&t, NULL, spin, (void *)3);
        usleep(1000);
        exit(code); /* exit_group with three threads running */
    case EXIT_FROM_THREAD:
        pthread_create(&t, NULL, exiter, (void *)(long)code);
        for (;;)
            pause();
    case KILLED:
        pthread_create(&t, NULL, sleeper, NULL);
        for (;;)
            pause(); /* the parent sends SIGTERM */
    case ORPHAN:
        if (fork() == 0) {
            usleep(2000); /* outlives its parent: init reaps it */
            _exit(0);
        }
        _exit(code);
    case EXEC:
        execl("/bin/echo", "echo", "-n", "", (char *)NULL); /* echo exits 0 */
        _exit(99);
    }
}

static int expected(int kind, int code, int st) {
    switch (kind) {
    case KILLED:
        return WIFSIGNALED(st) && WTERMSIG(st) == SIGTERM;
    case EXEC:
        return WIFEXITED(st) && WEXITSTATUS(st) == 0;
    }
    return WIFEXITED(st) && WEXITSTATUS(st) == code;
}

static int round_of(int r) {
    pid_t pids[KINDS];
    for (int k = 0; k < KINDS; k++) {
        int code = (r * KINDS + k) % 200 + 1;
        pids[k] = fork();
        if (pids[k] == 0)
            child(k, code);
        if (pids[k] < 0) {
            printf("waitstress: round %d: fork: %s\n", r, strerror(errno));
            return 1;
        }
    }
    struct timespec ms = {0, 1000000};
    nanosleep(&ms, NULL);
    kill(pids[KILLED], SIGTERM);
    for (int left = KINDS; left > 0;) {
        int st;
        pid_t pid = waitpid(-1, &st, 0);
        if (pid < 0 && errno == EINTR)
            continue;
        if (pid < 0) {
            printf("waitstress: round %d: waitpid: %s (%d left)\n", r, strerror(errno), left);
            return 1;
        }
        int k = 0;
        while (k < KINDS && pids[k] != pid)
            k++;
        if (k == KINDS || !expected(k, (r * KINDS + k) % 200 + 1, st)) {
            printf("waitstress: round %d: pid %d (kind %d) ended with status %#x\n", r, pid, k, st);
            return 1;
        }
        left--;
    }
    return 0;
}

int main(int argc, char **argv) {
    setvbuf(stdout, NULL, _IONBF, 0);
    int rounds = argc > 1 ? atoi(argv[1]) : 100;
    struct sigaction sa = {.sa_handler = on_chld};
    sigaction(SIGCHLD, &sa, NULL);
    for (int r = 0; r < rounds; r++) {
        if (round_of(r))
            return 1;
        if ((r + 1) % 50 == 0)
            printf("waitstress: %d rounds\n", r + 1);
    }
    printf("waitstress: %d rounds ok (%d SIGCHLD)\n", rounds, (int)chld);
    return 0;
}
