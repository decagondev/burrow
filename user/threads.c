/* threads.c - threads in one process (W12-S01), with musl's pthreads:
 * four workers add to a shared counter under a mutex, a producer and a
 * consumer hand items over through a condition variable, pthread_join
 * collects return values, and pthread_kill sends a signal to one worker,
 * whose handler sees its own thread-local storage. Prints "threads: ok"
 * when every check holds. Docs: docs/04-plan/waves/WAVE-12-ports.md. */
#define _GNU_SOURCE
#include <pthread.h>
#include <signal.h>
#include <stdio.h>
#include <string.h>
#include <sys/syscall.h>
#include <unistd.h>

#define WORKERS 4
#define ROUNDS 20000
#define ITEMS 50

static pthread_mutex_t lock = PTHREAD_MUTEX_INITIALIZER;
static long counter;

static pthread_cond_t ready = PTHREAD_COND_INITIALIZER;
static int queue[ITEMS], head, tail, done;

static __thread int who;              /* thread-local: which worker this is */
static volatile sig_atomic_t handled_by;
static volatile int go_on;

static void *add(void *arg) {
    who = (int)(long)arg;
    for (int i = 0; i < ROUNDS; i++) {
        pthread_mutex_lock(&lock);
        counter++;
        pthread_mutex_unlock(&lock);
    }
    return (void *)(long)(syscall(SYS_gettid));
}

static void *produce(void *arg) {
    (void)arg;
    for (int i = 1; i <= ITEMS; i++) {
        pthread_mutex_lock(&lock);
        queue[tail++ % ITEMS] = i;
        pthread_cond_signal(&ready);
        pthread_mutex_unlock(&lock);
    }
    pthread_mutex_lock(&lock);
    done = 1;
    pthread_cond_broadcast(&ready);
    pthread_mutex_unlock(&lock);
    return 0;
}

static void *consume(void *arg) {
    long sum = 0;
    (void)arg;
    pthread_mutex_lock(&lock);
    for (;;) {
        while (head == tail && !done) pthread_cond_wait(&ready, &lock);
        if (head == tail) break;
        sum += queue[head++ % ITEMS];
    }
    pthread_mutex_unlock(&lock);
    return (void *)sum;
}

static void on_usr1(int sig) { (void)sig; handled_by = who; }

static void *wait_for_signal(void *arg) {
    who = (int)(long)arg;
    while (!go_on) sched_yield();
    return 0;
}

int main(void) {
    int bad = 0;
    pthread_t t[WORKERS];
    long tids[WORKERS];
    for (long i = 0; i < WORKERS; i++) pthread_create(&t[i], 0, add, (void *)(i + 1));
    for (int i = 0; i < WORKERS; i++) {
        void *r;
        pthread_join(t[i], &r);
        tids[i] = (long)r;
    }
    printf("threads: counter %ld (want %d)\n", counter, WORKERS * ROUNDS);
    bad |= counter != WORKERS * ROUNDS;
    for (int i = 0; i < WORKERS; i++)
        for (int j = 0; j < i; j++) bad |= tids[i] == tids[j] || tids[i] == getpid();

    pthread_t p, c;
    void *sum;
    pthread_create(&c, 0, consume, 0);
    pthread_create(&p, 0, produce, 0);
    pthread_join(p, 0);
    pthread_join(c, &sum);
    printf("threads: consumer summed %ld (want %d)\n", (long)sum, ITEMS * (ITEMS + 1) / 2);
    bad |= (long)sum != ITEMS * (ITEMS + 1) / 2;

    struct sigaction sa;
    memset(&sa, 0, sizeof sa);
    sa.sa_handler = on_usr1;
    sigaction(SIGUSR1, &sa, 0);
    who = 0;
    pthread_t w;
    pthread_create(&w, 0, wait_for_signal, (void *)7L);
    pthread_kill(w, SIGUSR1);
    while (!handled_by) sched_yield();
    go_on = 1;
    pthread_join(w, 0);
    printf("threads: SIGUSR1 handled by worker %d (want 7)\n", (int)handled_by);
    bad |= handled_by != 7;

    puts(bad ? "threads: FAILED" : "threads: ok");
    return bad;
}
