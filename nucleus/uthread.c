/* uthread.c - see uthread.h. One lock covers the request FIFO, the waiting
 * dispatcher and every user thread's request state. */
#include "uthread.h"
#include "uspace.h"
#include "vmm.h"
#include "sched.h"
#include "syscall.h"
#include "trap.h"
#include "gdt.h"
#include "cpu.h"
#include "spinlock.h"
#include "errno.h"
#include "lib_string.h"

enum { REQ_NONE, REQ_QUEUED, REQ_TAKEN, REQ_DONE };

#define RFLAGS_IF_ON   (1ull << 9)
#define RFLAGS_FIXED   (1ull << 1)

static struct spinlock g_lock = SPINLOCK_INIT("uthread");
static struct thread *g_head, *g_tail;      /* QUEUED requests, oldest first */
static struct thread *g_waiter;             /* the dispatcher, blocked in uthread_wait */

static int user_addr(uint64_t a) { return a < USER_VA_TOP; }

long uthread_create(uint64_t as, uint64_t rip, uint64_t rsp, uint64_t fs) {
    struct addr_space *space = uspace_get(as);
    if (!space) return -EBADF;
    if (!user_addr(rip) || !user_addr(rsp) || !user_addr(fs)) return -EINVAL;
    struct trap_frame f;
    memset(&f, 0, sizeof f);
    f.rip = rip;
    f.cs = SEL_UCODE;
    f.rflags = RFLAGS_IF_ON | RFLAGS_FIXED;
    f.rsp = rsp;
    f.ss = SEL_UDATA;
    uspace_thread_ref(as, +1);
    struct thread *t = thread_create_user("user", &f, fs, space->pt.root, as);
    if (!t) {
        uspace_thread_ref(as, -1);
        return -EAGAIN;
    }
    return (long)thread_id(t);
}

/* Interrupts off. A user thread with this id, or 0. */
static struct thread *find_user(uint64_t tid) {
    struct thread *t = tid >> 32 ? 0 : thread_find((uint32_t)tid);
    return t && t->uspace ? t : 0;
}

static void unlink_req(struct thread *t) {
    for (struct thread **p = &g_head, *prev = 0; *p; prev = *p, p = &(*p)->req_next)
        if (*p == t) {
            *p = t->req_next;
            if (g_tail == t) g_tail = prev;
            t->req_next = 0;
            return;
        }
}

/* Leaves for good: uthread_kill joins the thread and frees its stack. */
static void __attribute__((noreturn)) die(void) { thread_exit(); }

/* Queues the current thread's request and blocks until Go answers. */
static long post_and_wait(uint64_t nr, const uint64_t args[6]) {
    struct thread *me = thread_current();
    uint64_t fl = spin_lock_irqsave(&g_lock);
    if (me->killed) { spin_unlock_irqrestore(&g_lock, fl); die(); }
    me->req[0] = thread_id(me);
    me->req[1] = nr;
    for (int i = 0; i < 6; i++) me->req[2 + i] = args[i];
    me->req_state = REQ_QUEUED;
    me->req_next = 0;
    if (g_tail) g_tail->req_next = me; else g_head = me;
    g_tail = me;
    if (g_waiter) { thread_wake(g_waiter); g_waiter = 0; }
    while (me->req_state != REQ_DONE && !me->killed) {
        thread_block_unlock(0, &g_lock);
        spin_lock_irqsave(&g_lock);              /* IF is already off: flags unused */
    }
    if (me->killed) { unlink_req(me); spin_unlock_irqrestore(&g_lock, fl); die(); }
    me->req_state = REQ_NONE;
    long r = me->req_result;
    spin_unlock_irqrestore(&g_lock, fl);
    return r;
}

long nu_user_syscall(struct syscall_frame *f) {
    const uint64_t args[6] = { SYSARG0(f), SYSARG1(f), SYSARG2(f), SYSARG3(f), SYSARG4(f), SYSARG5(f) };
    long r = post_and_wait(f->nr, args);
    if (!user_addr(f->rip)) {           /* PIT-24: SYSRET to a non-canonical RIP #GPs in ring 0 */
        const uint64_t fault[6] = { 0, f->rip, 0, 0, 0, 0 };
        post_and_wait(UREQ_FAULT | 13, fault);
        die();
    }
    return r;
}

void uthread_fault(const struct trap_frame *f, uint64_t cr2) {
    const uint64_t args[6] = { cr2, f->rip, f->error, 0, 0, 0 };
    post_and_wait(UREQ_FAULT | f->vector, args);
    die();                              /* an answered fault still cannot resume */
}

void uthread_check_killed(void) {
    if (thread_current()->killed) die();
}

long uthread_wait(uint64_t *buf, uint64_t max) {
    struct thread *me = thread_current();
    uint64_t fl = spin_lock_irqsave(&g_lock);
    while (!g_head) {
        if (g_waiter && g_waiter != me) { spin_unlock_irqrestore(&g_lock, fl); return -EBUSY; }
        g_waiter = me;
        thread_block_unlock(0, &g_lock);
        spin_lock_irqsave(&g_lock);
    }
    if (g_waiter == me) g_waiter = 0;
    long n = 0;
    while (g_head && (uint64_t)n < max) {
        struct thread *t = g_head;
        g_head = t->req_next;
        if (!g_head) g_tail = 0;
        t->req_next = 0;
        t->req_state = REQ_TAKEN;
        memcpy(buf + n * UREQ_WORDS, t->req, sizeof t->req);
        n++;
    }
    spin_unlock_irqrestore(&g_lock, fl);
    return n;
}

long uthread_reply(uint64_t tid, long result) {
    uint64_t fl = spin_lock_irqsave(&g_lock);
    struct thread *t = find_user(tid);
    long rc = -ESRCH;
    if (t && t->req_state == REQ_TAKEN) {
        t->req_result = result;
        t->req_state = REQ_DONE;
        thread_wake(t);
        rc = 0;
    }
    spin_unlock_irqrestore(&g_lock, fl);
    return rc;
}

long uthread_kill(uint64_t tid) {
    uint64_t fl = spin_lock_irqsave(&g_lock);
    struct thread *t = find_user(tid);
    if (!t) { spin_unlock_irqrestore(&g_lock, fl); return -ESRCH; }
    t->killed = 1;
    if (t->req_state == REQ_QUEUED) unlink_req(t);      /* Go must not take it any more */
    thread_wake(t);                                     /* if it waits for an answer */
    uint64_t as = t->uspace;
    spin_unlock_irqrestore(&g_lock, fl);
    thread_join(t);                     /* it leaves at its next pass through the kernel */
    uspace_thread_ref(as, -1);
    return 0;
}

long uthread_set_fs(uint64_t tid, uint64_t fs) {
    if (!user_addr(fs)) return -EINVAL;   /* a non-canonical FS_BASE would #GP the wrmsr */
    uint64_t fl = spin_lock_irqsave(&g_lock);
    struct thread *t = find_user(tid);
    if (t) thread_set_fs_base(t, fs);
    spin_unlock_irqrestore(&g_lock, fl);
    return t ? 0 : -ESRCH;
}
