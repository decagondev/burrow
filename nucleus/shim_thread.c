/* shim_thread.c - threads, sleeping and futexes for the Go runtime's Ms:
 * every M is a nucleus thread, every runtime lock and note a futex. */
#include "shim.h"
#include "sched.h"
#include "futex.h"
#include "trap.h"
#include "gdt.h"
#include "errno.h"
#include "lib_string.h"

#define CLONE_VM      0x100
#define CLONE_THREAD  0x10000
#define CLONE_SETTLS  0x80000

/* clone(flags, stack, ptid, ctid, tls): a new thread in the same address
 * space. The child resumes right after the SYSCALL with every register as
 * the parent had it (Go's runtime.clone keeps m, g and fn in R13, R9, R12),
 * except RAX = 0 and RSP = stack. RCX/R11 hold RIP/RFLAGS, as on Linux. */
long sys_clone(struct syscall_frame *f) {
    uint64_t flags = SYSARG0(f);
    if ((flags & (CLONE_VM | CLONE_THREAD)) != (CLONE_VM | CLONE_THREAD))
        return -ENOSYS;                                  /* fork() arrives with processes */
    struct trap_frame c;
    memset(&c, 0, sizeof c);
    c.r15 = f->r15; c.r14 = f->r14; c.r13 = f->r13; c.r12 = f->r12;
    c.rbp = f->rbp; c.rbx = f->rbx;
    c.r9 = f->r9; c.r8 = f->r8; c.r10 = f->r10;
    c.rdx = f->rdx; c.rsi = f->rsi; c.rdi = f->rdi;
    c.rcx = f->rip; c.r11 = f->rflags; c.rax = 0;
    c.rip = f->rip; c.cs = SEL_KCODE; c.rflags = f->rflags;
    c.rsp = SYSARG1(f) ? SYSARG1(f) : f->rsp; c.ss = SEL_KDATA;
    uint64_t tls = flags & CLONE_SETTLS ? SYSARG4(f) : thread_current()->fs_base;
    struct thread *t = thread_create_from_frame("go-m", &c, tls);
    return t ? (long)thread_id(t) : -EAGAIN;
}

/* exit(code): this thread only (Go: an M that is done, e.g. after LockOSThread). */
long sys_exit(struct syscall_frame *f) { (void)f; thread_exit(); }

/* struct timespec {sec, nsec} -> ns; a negative or absurd value -> -1. */
static int64_t timespec_ns(const int64_t *ts) {
    if (ts[0] < 0 || ts[1] < 0 || ts[1] >= 1000000000ll) return -1;
    return ts[0] * 1000000000ll + ts[1];
}

#define FUTEX_WAIT         0
#define FUTEX_WAKE         1
#define FUTEX_PRIVATE_FLAG 128          /* one address space anyway */

/* futex(addr, op, val, timeout): WAIT with a relative timespec (or none)
 * and WAKE; everything the Go runtime's lock_futex.go uses. */
long sys_futex(struct syscall_frame *f) {
    uint32_t *addr = (uint32_t *)SYSARG0(f);
    int op = (int)SYSARG1(f) & ~FUTEX_PRIVATE_FLAG;
    uint32_t val = (uint32_t)SYSARG2(f);
    if (op == FUTEX_WAKE) return futex_wake(addr, val);
    if (op != FUTEX_WAIT) return -ENOSYS;
    const int64_t *ts = (const int64_t *)SYSARG3(f);
    if (!ts) return futex_wait(addr, val, 0);
    int64_t ns = timespec_ns(ts);
    if (ns < 0) return -EINVAL;
    return futex_wait(addr, val, ns ? (uint64_t)ns : 1);   /* 0 means "do not wait", not forever */
}

long sys_sched_yield(struct syscall_frame *f) { (void)f; thread_yield(); return 0; }

long sys_nanosleep(struct syscall_frame *f) {
    int64_t ns = timespec_ns((const int64_t *)SYSARG0(f));
    if (ns < 0) return -EINVAL;
    thread_sleep_ns((uint64_t)ns);
    return 0;
}
