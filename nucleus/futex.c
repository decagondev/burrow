/* futex.c - see futex.h. 64 hash buckets, each a FIFO-ish singly linked
 * list of blocked threads (struct thread.wait_next) and its own lock. */
#include "futex.h"
#include "sched.h"
#include "spinlock.h"
#include "errno.h"

#define BUCKETS 64u

struct bucket { struct spinlock lock; struct thread *head; };
static struct bucket g_buckets[BUCKETS];

static struct bucket *bucket_for(const uint32_t *addr) {
    uint64_t k = (uint64_t)(uintptr_t)addr >> 2;
    return &g_buckets[(k ^ (k >> 6) ^ (k >> 12)) % BUCKETS];
}

/* Removes t from b's list; 1 if it was there. Caller holds b->lock. */
static int unlink(struct bucket *b, struct thread *t) {
    for (struct thread **p = &b->head; *p; p = &(*p)->wait_next)
        if (*p == t) { *p = t->wait_next; t->wait_next = 0; return 1; }
    return 0;
}

long futex_wait(uint32_t *addr, uint32_t expected, uint64_t timeout_ns) {
    struct bucket *b = bucket_for(addr);
    struct thread *me = thread_current();
    uint64_t f = spin_lock_irqsave(&b->lock);
    if (__atomic_load_n(addr, __ATOMIC_ACQUIRE) != expected) {
        spin_unlock_irqrestore(&b->lock, f);
        return -EAGAIN;
    }
    me->futex_key = (uint64_t)(uintptr_t)addr;
    me->wait_next = b->head;
    b->head = me;
    thread_block_unlock(timeout_ns, &b->lock);   /* atomic: blocked before the lock drops */
    long rc = 0;
    if (me->timed_out) {
        spin_lock_irqsave(&b->lock);              /* IF is already off: flags unused */
        if (unlink(b, me)) rc = -ETIMEDOUT;       /* not in the list: a wake won the race */
        spin_unlock(&b->lock);
    }
    irq_restore(f);
    return rc;
}

long futex_wake(uint32_t *addr, uint32_t count) {
    struct bucket *b = bucket_for(addr);
    uint64_t key = (uint64_t)(uintptr_t)addr;
    long woken = 0;
    uint64_t f = spin_lock_irqsave(&b->lock);
    for (struct thread **p = &b->head; *p && (uint32_t)woken < count;) {
        struct thread *t = *p;
        if (t->futex_key != key) { p = &t->wait_next; continue; }
        *p = t->wait_next;
        t->wait_next = 0;
        thread_wake(t);
        woken++;
    }
    spin_unlock_irqrestore(&b->lock, f);
    return woken;
}
