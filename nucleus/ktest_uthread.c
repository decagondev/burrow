/* ktest_uthread.c - ring 3 (W9-S02). The ktest plays the Go dispatcher:
 * it builds a tiny process from hand-assembled machine code, takes its
 * requests with nu_syscall_wait and answers or kills it. */
#include "ktest.h"
#include "ktest_nu.h"
#include "uspace.h"
#include "uthread.h"
#include "sched.h"
#include "frames.h"
#include "boot.h"
#include "errno.h"

#define CODE_VA    0x400000ull
#define STACK_VA   0x7FFF0000ull
#define STACK_TOP  (STACK_VA + 0x1000)
#define FAULT(v)   (UREQ_FAULT | (v))
#define MS         (1000ull * 1000)

struct uproc { long as, code, stack; };

/* An address space with `code` at va (read + execute) and a stack page. */
static int uproc_new(struct uproc *p, const uint8_t *code, uint64_t len, uint64_t va) {
    p->as = nu(0x1010, 0, 0, 0, 0, 0);
    p->code = nu(0x1014, 1, 0, 0, 0, 0);
    p->stack = nu(0x1014, 1, 0, 0, 0, 0);
    if (p->as < 1 || p->code <= 0 || p->stack <= 0) return -1;
    uint8_t *dst = phys_to_virt((uint64_t)p->code);
    for (uint64_t i = 0; i < len; i++) dst[(va & 0xFFF) + i] = code[i];
    if (nu(0x1011, p->as, (long)(va & ~0xFFFull), p->code, 4096, UPROT_EXEC) != 0) return -1;
    return nu(0x1011, p->as, STACK_VA, p->stack, 4096, UPROT_WRITE) == 0 ? 0 : -1;
}

static int uproc_free(struct uproc *p) {
    if (nu(0x1016, p->as, 0, 0, 0, 0) != 0) return -1;
    nu(0x1015, p->code, 1, 0, 0, 0);
    nu(0x1015, p->stack, 1, 0, 0, 0);
    return 0;
}

static uint64_t g_req[4 * UREQ_WORDS];

static long next_req(void) { return nu(0x1021, (long)g_req, 4, 0, 0, 0); }

/* getpid, then exit(getpid + 1): two requests through the queue, SYSRETQ between. */
int kt_uthread_syscalls(void) {
    static const uint8_t code[] = {
        0xB8, 39, 0, 0, 0, 0x0F, 0x05,        /* mov eax, 39 ; syscall */
        0x8D, 0x78, 0x01,                     /* lea edi, [rax + 1]    */
        0xB8, 60, 0, 0, 0, 0x0F, 0x05,        /* mov eax, 60 ; syscall */
        0xEB, 0xFE,                           /* jmp $                 */
    };
    uint64_t free0 = frames_free_count();
    struct uproc p;
    KT_CHECK(uproc_new(&p, code, sizeof code, CODE_VA) == 0);
    long tid = nu(0x1020, p.as, CODE_VA, STACK_TOP, 0, 0);
    KT_CHECK(tid > 0);
    KT_CHECK(nu(0x1016, p.as, 0, 0, 0, 0) == -EBUSY);           /* a thread runs in it */
    KT_CHECK(next_req() == 1 && g_req[0] == (uint64_t)tid && g_req[1] == 39);
    KT_CHECK(nu(0x1022, tid, 41, 0, 0, 0) == 0);
    KT_CHECK(nu(0x1022, tid, 41, 0, 0, 0) == -ESRCH);           /* already answered */
    KT_CHECK(next_req() == 1 && g_req[1] == 60 && g_req[2] == 42);
    KT_CHECK(nu(0x1023, tid, 0, 0, 0, 0) == 0);
    KT_CHECK(nu(0x1023, tid, 0, 0, 0, 0) == -ESRCH);
    KT_CHECK(uproc_free(&p) == 0);
    KT_CHECK(frames_free_count() == free0);                     /* stack, tables, pages */
    return 0;
}

/* An exception in ring 3 becomes a fault request; the nucleus keeps running. */
static int expect_fault(const uint8_t *code, uint64_t len, uint64_t vector, uint64_t cr2) {
    struct uproc p;
    KT_CHECK(uproc_new(&p, code, len, CODE_VA) == 0);
    long tid = nu(0x1020, p.as, CODE_VA, STACK_TOP, 0, 0);
    KT_CHECK(tid > 0);
    KT_CHECK(next_req() == 1 && g_req[0] == (uint64_t)tid && g_req[1] == FAULT(vector));
    KT_CHECK(g_req[2] == cr2 && g_req[3] == CODE_VA);
    KT_CHECK(nu(0x1023, tid, 0, 0, 0, 0) == 0);
    KT_CHECK(uproc_free(&p) == 0);
    return 0;
}

int kt_uthread_faults(void) {
    static const uint8_t null_read[] = { 0x48, 0x8B, 0x04, 0x25, 0, 0, 0, 0 };   /* mov rax, [0] */
    static const uint8_t kernel_read[] = {                                      /* mov rax, [nucleus] */
        0x48, 0xA1, 0x00, 0x00, 0x10, 0x80, 0xFF, 0xFF, 0xFF, 0xFF };
    static const uint8_t ud2[] = { 0x0F, 0x0B };
    KT_CHECK(expect_fault(null_read, sizeof null_read, 14, 0) == 0);
    KT_CHECK(expect_fault(kernel_read, sizeof kernel_read, 14, 0xFFFFFFFF80100000ull) == 0);
    KT_CHECK(expect_fault(ud2, sizeof ud2, 6, 0) == 0);
    return 0;
}

/* A thread spinning in ring 3 is preempted by the timer and can be killed. */
int kt_uthread_preempt_kill(void) {
    static const uint8_t spin[] = {
        0x48, 0xFF, 0x04, 0x25, 0x00, 0x00, 0xFF, 0x7F,   /* inc qword [0x7FFF0000] */
        0xEB, 0xF6,                                       /* jmp back               */
    };
    struct uproc p;
    KT_CHECK(uproc_new(&p, spin, sizeof spin, CODE_VA) == 0);
    long tid = nu(0x1020, p.as, CODE_VA, STACK_TOP, 0, 0);
    KT_CHECK(tid > 0);
    thread_sleep_ns(30 * MS);                                 /* the user thread runs meanwhile */
    volatile uint64_t *counter = phys_to_virt((uint64_t)p.stack);
    KT_CHECK(*counter > 1000);
    KT_CHECK(nu(0x1023, tid, 0, 0, 0, 0) == 0);
    uint64_t after = *counter;
    thread_sleep_ns(5 * MS);
    KT_CHECK(*counter == after);                              /* really gone */
    KT_CHECK(uproc_free(&p) == 0);
    return 0;
}

/* PIT-24: a SYSCALL in the last bytes of user space returns to a
 * non-canonical RIP; SYSRET would #GP in ring 0, so it becomes a fault. */
int kt_uthread_noncanonical_return(void) {
    static const uint8_t code[] = { 0xB8, 39, 0, 0, 0, 0x0F, 0x05 };   /* ends at 2^47 */
    const uint64_t at = USER_VA_TOP - sizeof code;
    struct uproc p;
    KT_CHECK(uproc_new(&p, code, sizeof code, at) == 0);
    long tid = nu(0x1020, p.as, (long)at, STACK_TOP, 0, 0);
    KT_CHECK(tid > 0);
    KT_CHECK(next_req() == 1 && g_req[1] == 39);
    KT_CHECK(nu(0x1022, tid, 0, 0, 0, 0) == 0);
    KT_CHECK(next_req() == 1 && g_req[1] == FAULT(13) && g_req[3] == USER_VA_TOP);
    KT_CHECK(nu(0x1023, tid, 0, 0, 0, 0) == 0);
    KT_CHECK(uproc_free(&p) == 0);
    return 0;
}

/* The FS base given at creation is the thread's TLS: exit(*(fs:0)). */
int kt_uthread_fs_base(void) {
    static const uint8_t code[] = {
        0x64, 0x48, 0x8B, 0x04, 0x25, 0, 0, 0, 0,   /* mov rax, fs:[0] */
        0x48, 0x89, 0xC7,                           /* mov rdi, rax    */
        0xB8, 60, 0, 0, 0, 0x0F, 0x05,              /* exit            */
    };
    struct uproc p;
    KT_CHECK(uproc_new(&p, code, sizeof code, CODE_VA) == 0);
    *(volatile uint64_t *)((uint8_t *)phys_to_virt((uint64_t)p.stack) + 0x800) = 0x5EED;
    long tid = nu(0x1020, p.as, CODE_VA, STACK_TOP, STACK_VA + 0x800, 0);
    KT_CHECK(tid > 0);
    KT_CHECK(next_req() == 1 && g_req[1] == 60 && g_req[2] == 0x5EED);
    KT_CHECK(nu(0x1024, tid, (long)USER_VA_TOP, 0, 0, 0) == -EINVAL);
    KT_CHECK(nu(0x1024, 9999, 0, 0, 0, 0) == -ESRCH);
    KT_CHECK(nu(0x1024, tid, 0x1000, 0, 0, 0) == 0);
    KT_CHECK(nu(0x1023, tid, 0, 0, 0, 0) == 0);
    KT_CHECK(nu(0x1020, p.as, (long)USER_VA_TOP, STACK_TOP, 0, 0) == -EINVAL);
    KT_CHECK(uproc_free(&p) == 0);
    return 0;
}
