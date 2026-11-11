/* uthread.c - see uthread.h. One lock covers the request FIFO, the waiting
 * dispatcher and every user thread's request state. */
#include "uthread.h"
#include "uspace.h"
#include "vmm.h"
#include "sched.h"
#include "trap.h"
#include "gdt.h"
#include "cpu.h"
#include "spinlock.h"
#include "errno.h"
#include "lib_string.h"
#include "cputime.h"
#include "lattrace.h"
#include "time.h"

enum { REQ_NONE, REQ_QUEUED, REQ_TAKEN, REQ_DONE };
_Static_assert(LAT_STAMPS == sizeof ((struct thread *)0)->lat / sizeof(uint64_t), "struct thread's stamps");

#define RFLAGS_IF_ON   (1ull << 9)
#define RFLAGS_FIXED   (1ull << 1)

static struct spinlock g_lock = SPINLOCK_INIT("uthread");
static struct lat_stats g_lat;                 /* W25-S01: the traced call's step sums, under g_lock */
#define LAT_GO_CALLS 512                       /* SYS_MAX: the Go kernel's own calls while tracing (W25-S03) */
static uint64_t g_lat_go[LAT_GO_CALLS];
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
    struct thread *t = thread_create_user("user", &f, fs, space->pt.root, as, 0);
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

struct thread *uthread_held(uint64_t tid, uint64_t *flags) {
    *flags = spin_lock_irqsave(&g_lock);
    struct thread *t = find_user(tid);
    return t && t->req_state == REQ_TAKEN && t->uframe ? t : 0;
}

void uthread_unlock(uint64_t flags) { spin_unlock_irqrestore(&g_lock, flags); }

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

/* Queues the current thread's request (its registers are *f) and blocks
 * until Go answers; returns the answer. A killed thread never returns. */
static long post_and_wait(struct trap_frame *f, uint64_t nr, const uint64_t args[6]) {
    struct thread *me = thread_current();
    uint64_t fl = spin_lock_irqsave(&g_lock);
    if (me->killed) { spin_unlock_irqrestore(&g_lock, fl); die(); }
    me->req[0] = thread_id(me);
    me->req[1] = nr;
    for (int i = 0; i < 6; i++) me->req[2 + i] = args[i];
    me->uframe = f;
    me->interrupt = 0;                       /* Go sees the thread now: it can deliver */
    me->req_state = REQ_QUEUED;
    me->req_next = 0;
    if (g_tail) g_tail->req_next = me; else g_head = me;
    g_tail = me;
    if (g_waiter) { thread_wake(g_waiter); g_waiter = 0; }
    while (me->req_state != REQ_DONE && !me->killed) {
        thread_block_unlock(0, &g_lock);
        spin_lock_irqsave(&g_lock);              /* IF is already off: flags unused */
    }
    if (me->lat[LAT_GATE]) me->lat[LAT_RESUMED] = rdtsc();
    if (me->killed) { unlink_req(me); spin_unlock_irqrestore(&g_lock, fl); die(); }
    me->req_state = REQ_NONE;
    me->uframe = 0;
    long r = me->req_result;
    spin_unlock_irqrestore(&g_lock, fl);
    return r;
}

/* W25-S01: a traced request's last stamp, on its way back to ring 3. */
static void lat_out(struct thread *me) {
    uint64_t out = rdtsc();
    uint64_t fl = spin_lock_irqsave(&g_lock);
    if (g_lat.on) lat_record(&g_lat, me->lat, out);
    spin_unlock_irqrestore(&g_lock, fl);
    me->lat[LAT_GATE] = 0;
}

void nu_user_syscall(struct trap_frame *f) {
    struct thread *me = thread_current();
    cputime_enter_kernel(me);   /* W23-S02 */
    me->lat[LAT_GATE] = 0;
    if (g_lat.on && lat_wanted(&g_lat, f->rax)) {   /* W25-S01: stamped only while traced */
        me->lat[LAT_TAKEN] = me->lat[LAT_REPLIED] = me->lat[LAT_RESUMED] = 0;
        me->lat[LAT_GATE] = rdtsc();
    }
    const uint64_t args[6] = { f->rdi, f->rsi, f->rdx, f->r10, f->r8, f->r9 };
    f->rax = (uint64_t)post_and_wait(f, f->rax, args);
    uthread_return_to_user(f);
    if (me->lat[LAT_GATE]) lat_out(me);
}

void uthread_fault(struct trap_frame *f, uint64_t cr2) {
    const uint64_t args[6] = { cr2, f->rip, f->error, 0, 0, 0 };
    post_and_wait(f, UREQ_FAULT | f->vector, args);   /* only a syscall's answer is RAX */
    uthread_return_to_user(f);          /* answered: resume (Go fixed the cause or the registers) */
}

/* Every way back to ring 3 passes here: a killed thread leaves, a thread
 * Go wants to stop posts an interrupt request, and the frame must be one
 * IRETQ can return to (PIT-24: a non-canonical RIP would #GP in ring 0). */
void uthread_return_to_user(struct trap_frame *f) {
    struct thread *me = thread_current();
    for (;;) {
        if (me->killed) die();
        if (!user_addr(f->rip) || !user_addr(f->rsp)) {
            const uint64_t args[6] = { 0, f->rip, 0, 0, 0, 0 };
            f->rip = 0;                  /* never return there, whatever Go answers */
            post_and_wait(f, UREQ_FAULT | 13, args);
            if (!user_addr(f->rip) || f->rip == 0) die();
            continue;
        }
        if (!me->interrupt) {
            cputime_enter_user(me);      /* W23-S02: back to ring 3 */
            return;
        }
        const uint64_t args[6] = { f->rip, 0, 0, 0, 0, 0 };
        post_and_wait(f, UREQ_INTERRUPT, args);   /* the answer is not a register: */
    }                                             /* Go changes them with nu_uthread_regs */
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
        if (t->lat[LAT_GATE]) t->lat[LAT_TAKEN] = rdtsc();
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
        if (t->lat[LAT_GATE]) t->lat[LAT_REPLIED] = rdtsc();
        t->req_result = result;
        t->req_state = REQ_DONE;
        thread_wake(t);
        rc = 0;
    }
    spin_unlock_irqrestore(&g_lock, fl);
    return rc;
}

/* Kills a user thread and returns once it is gone. Two kills of one thread
 * at once (BUG-0023): the first joins it and drops its address space's
 * thread count; the second only waits for the join to finish, so neither
 * happens twice and neither caller returns while the thread still runs. */
long uthread_kill(uint64_t tid) {
    uint64_t fl = spin_lock_irqsave(&g_lock);
    struct thread *t = find_user(tid);
    if (!t) { spin_unlock_irqrestore(&g_lock, fl); return -ESRCH; }
    uint32_t id = thread_id(t);
    if (t->killed) {                                    /* another kill owns the join */
        spin_unlock_irqrestore(&g_lock, fl);
        thread_join_id(t, id);                          /* returns when that join freed it */
        return 0;
    }
    t->killed = 1;
    if (t->req_state == REQ_QUEUED) unlink_req(t);      /* Go must not take it any more */
    thread_wake(t);                                     /* if it waits for an answer */
    thread_poke(t);                                     /* if it runs on another CPU */
    uint64_t as = t->uspace;
    spin_unlock_irqrestore(&g_lock, fl);
    thread_join_id(t, id);              /* it leaves at its next pass through the kernel */
    uspace_thread_ref(as, -1);
    return 0;
}

/* The thread stops at its next return to ring 3: at once if it computes on
 * another CPU (a wake IPI), at its next tick if on this one, or right after
 * Go answers the request it is waiting on. */
long uthread_interrupt(uint64_t tid) {
    uint64_t fl = spin_lock_irqsave(&g_lock);
    struct thread *t = find_user(tid);
    if (t) {
        t->interrupt = 1;
        thread_poke(t);                 /* running elsewhere: stop at once, not at its tick */
    }
    spin_unlock_irqrestore(&g_lock, fl);
    return t ? 0 : -ESRCH;
}

long uthread_set_fs(uint64_t tid, uint64_t fs) {
    if (!user_addr(fs)) return -EINVAL;   /* a non-canonical FS_BASE would #GP the wrmsr */
    uint64_t fl = spin_lock_irqsave(&g_lock);
    struct thread *t = find_user(tid);
    if (t) thread_set_fs_base(t, fs);
    spin_unlock_irqrestore(&g_lock, fl);
    return t ? 0 : -ESRCH;
}

/* W25-S03: one of the Go kernel's own calls (the shim's), counted while a
 * user call is traced: what Go spends its share of a round trip on. */
void uthread_lat_go_call(uint64_t nr) {
    if (g_lat.on && nr < LAT_GO_CALLS) __atomic_fetch_add(&g_lat_go[nr], 1, __ATOMIC_RELAXED);
}

/* nu_lattrace (W25-S01): op 0 traces call nr arg (and clears the sums), op
 * 1 stops, op 2 copies {on, nr, count, skipped, step ns x4} into buf, op 3
 * (W25-S03) the Go kernel's own calls by number (512 words). */
long uthread_lattrace(uint64_t op, uint64_t arg, uint64_t *buf) {
    long rc = 0;
    uint64_t fl = spin_lock_irqsave(&g_lock);
    switch (op) {
    case 0:
        lat_start(&g_lat, arg);
        for (int i = 0; i < LAT_GO_CALLS; i++) __atomic_store_n(&g_lat_go[i], 0, __ATOMIC_RELAXED);
        break;
    case 1: lat_stop(&g_lat); break;
    case 2:
        buf[0] = (uint64_t)g_lat.on;
        buf[1] = g_lat.nr;
        buf[2] = g_lat.count;
        buf[3] = g_lat.skipped;
        for (int i = 0; i < LAT_STEPS; i++) buf[4 + i] = time_ticks_to_ns(g_lat.sum[i]);
        break;
    case 3:   /* W25-S03: the Go kernel's own calls by number, LAT_GO_CALLS words */
        for (int i = 0; i < LAT_GO_CALLS; i++) buf[i] = __atomic_load_n(&g_lat_go[i], __ATOMIC_RELAXED);
        break;
    default: rc = -EINVAL;
    }
    spin_unlock_irqrestore(&g_lock, fl);
    return rc;
}
