/* ktest_uregs.c - W10-S01: Go's view of a stopped user thread. The ktest
 * plays the Go kernel: it rewrites registers (signals), resumes a fault,
 * stops a busy thread, forks and execs, all with hand-assembled code. */
#include "ktest.h"
#include "ktest_nu.h"
#include "uspace.h"
#include "uthread.h"
#include "sched.h"
#include "boot.h"
#include "errno.h"
#include "lib_string.h"

#define INTERRUPT 0xFFFF0100ull
#define MS        (1000ull * 1000)

static struct uregs g_regs __attribute__((aligned(16)));

static long regs(long tid, int write) { return nu(0x1025, tid, (long)&g_regs, write, 0, 0); }

/* getpid; exit(rax)   ...   at +0x20: exit(rbx) */
static const uint8_t two_exits[0x2C] = {
    0xB8, 39, 0, 0, 0, 0x0F, 0x05,            /* 0x00 mov eax, 39 ; syscall */
    0x48, 0x89, 0xC7,                         /* 0x07 mov rdi, rax          */
    0xB8, 60, 0, 0, 0, 0x0F, 0x05,            /* 0x0A exit(rdi)             */
    0xEB, 0xFE,                               /* 0x11 jmp $                 */
    0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0,    /* pad 0x13..0x1F             */
    0x48, 0x89, 0xDF,                         /* 0x20 mov rdi, rbx          */
    0xB8, 60, 0, 0, 0, 0x0F, 0x05,            /* 0x23 exit(rdi)             */
    0xEB, 0xFE,
};

/* The waiting thread's registers come out whole and go back in checked:
 * a new RIP and RBX take it to the second exit (IRETQ restores them all). */
int kt_uthread_regs(void) {
    struct uproc p;
    KT_CHECK(uproc_new(&p, two_exits, sizeof two_exits, CODE_VA) == 0);
    long tid = nu(0x1020, p.as, CODE_VA, STACK_TOP, 0, 0);
    KT_CHECK(tid > 0);
    KT_CHECK(next_req() == 1 && g_req[1] == 39);
    KT_CHECK(regs(tid, 0) == 0);
    KT_CHECK(g_regs.rip == CODE_VA + 7 && g_regs.rcx == CODE_VA + 7);   /* SYSCALL's RCX */
    KT_CHECK(g_regs.rax == 39 && g_regs.rsp == STACK_TOP && (g_regs.rflags & (1u << 9)));
    g_regs.rip = CODE_VA + 0x20;
    g_regs.rbx = 0xCAFE;
    g_regs.rflags |= 3ull << 12;                              /* IOPL 3: must be dropped */
    KT_CHECK(regs(tid, 1) == 0);
    KT_CHECK(regs(tid, 0) == 0 && !(g_regs.rflags & (3ull << 12)));
    g_regs.rip = USER_VA_TOP;
    KT_CHECK(regs(tid, 1) == -EINVAL);                        /* refused, frame unchanged */
    KT_CHECK(nu(0x1022, tid, 0, 0, 0, 0) == 0);
    KT_CHECK(next_req() == 1 && g_req[1] == 60 && g_req[2] == 0xCAFE);
    KT_CHECK(regs(9999, 0) == -EBUSY);
    KT_CHECK(nu(0x1023, tid, 0, 0, 0, 0) == 0);
    KT_CHECK(uproc_free(&p) == 0);
    return 0;
}

/* A write to a read-only page faults; Go makes it writable and answers;
 * the instruction runs again and the thread goes on. */
int kt_uthread_fault_resumes(void) {
    static const uint8_t code[] = {
        0x48, 0xC7, 0x04, 0x25, 0x00, 0x00, 0xFE, 0x7F, 0x2A, 0, 0, 0,  /* mov qword [0x7FFE0000], 42 */
        0x48, 0x8B, 0x3C, 0x25, 0x00, 0x00, 0xFE, 0x7F,                  /* mov rdi, [0x7FFE0000]      */
        0xB8, 60, 0, 0, 0, 0x0F, 0x05, 0xEB, 0xFE,                       /* exit(rdi)                  */
    };
    struct uproc p;
    KT_CHECK(uproc_new(&p, code, sizeof code, CODE_VA) == 0);
    long data = nu(0x1014, 1, 0, 0, 0, 0);
    KT_CHECK(data > 0 && nu(0x1011, p.as, 0x7FFE0000, data, 4096, 0) == 0);   /* read-only */
    long tid = nu(0x1020, p.as, CODE_VA, STACK_TOP, 0, 0);
    KT_CHECK(tid > 0);
    KT_CHECK(next_req() == 1 && g_req[1] == FAULT(14) && g_req[2] == 0x7FFE0000);
    KT_CHECK((g_req[4] & 7) == 7);                            /* present, write, user */
    KT_CHECK(nu(0x1012, p.as, 0x7FFE0000, 4096, 0, 0) == 0);
    KT_CHECK(nu(0x1011, p.as, 0x7FFE0000, data, 4096, UPROT_WRITE) == 0);
    KT_CHECK(nu(0x1022, tid, 0, 0, 0, 0) == 0);              /* resume */
    KT_CHECK(next_req() == 1 && g_req[1] == 60 && g_req[2] == 42);
    KT_CHECK(nu(0x1023, tid, 0, 0, 0, 0) == 0);
    KT_CHECK(uproc_free(&p) == 0);
    nu(0x1015, data, 1, 0, 0, 0);
    return 0;
}

/* A thread computing in ring 3, with no system calls, is stopped on
 * request; Go sends it elsewhere (a signal handler, later). */
int kt_uthread_interrupt(void) {
    static uint8_t code[0x30] = {
        0x48, 0xFF, 0x04, 0x25, 0x00, 0x00, 0xFF, 0x7F, 0xEB, 0xF6,   /* inc qword [0x7FFF0000]; loop */
    };
    memcpy(code + 0x20, two_exits + 0x20, 10);                     /* +0x20: exit(rbx) */
    struct uproc p;
    KT_CHECK(uproc_new(&p, code, sizeof code, CODE_VA) == 0);
    long tid = nu(0x1020, p.as, CODE_VA, STACK_TOP, 0, 0);
    KT_CHECK(tid > 0);
    thread_sleep_ns(20 * MS);
    KT_CHECK(nu(0x1028, tid, 0, 0, 0, 0) == 0);
    KT_CHECK(next_req() == 1 && g_req[0] == (uint64_t)tid && g_req[1] == INTERRUPT);
    KT_CHECK(g_req[2] >= CODE_VA && g_req[2] < CODE_VA + 10);       /* somewhere in the loop */
    KT_CHECK(regs(tid, 0) == 0);
    g_regs.rip = CODE_VA + 0x20;
    g_regs.rbx = 7;
    KT_CHECK(regs(tid, 1) == 0);
    KT_CHECK(nu(0x1022, tid, 0, 0, 0, 0) == 0);
    KT_CHECK(next_req() == 1 && g_req[1] == 60 && g_req[2] == 7);
    KT_CHECK(nu(0x1028, 9999, 0, 0, 0, 0) == -ESRCH);
    KT_CHECK(nu(0x1023, tid, 0, 0, 0, 0) == 0);
    KT_CHECK(uproc_free(&p) == 0);
    return 0;
}

/* fork: a second thread in a second address space starts from the
 * parent's registers with RAX = 0; execve: the parent moves to a third
 * space whose code exits with 0x33. */
int kt_uthread_fork_exec(void) {
    static const uint8_t other[] = { 0xBF, 0x33, 0, 0, 0, 0xB8, 60, 0, 0, 0, 0x0F, 0x05, 0xEB, 0xFE };
    struct uproc parent, child, image;
    KT_CHECK(uproc_new(&parent, two_exits, sizeof two_exits, CODE_VA) == 0);
    KT_CHECK(uproc_new(&child, two_exits, sizeof two_exits, CODE_VA) == 0);
    KT_CHECK(uproc_new(&image, other, sizeof other, CODE_VA) == 0);
    long tid = nu(0x1020, parent.as, CODE_VA, STACK_TOP, 0, 0);
    KT_CHECK(tid > 0);
    KT_CHECK(next_req() == 1 && g_req[1] == 39);
    KT_CHECK(regs(tid, 0) == 0);
    g_regs.rax = 0;                                           /* the child's fork() returns 0 */
    *(uint32_t *)(g_regs.fx + 24) = 0xFFFFFFFF;               /* bad MXCSR: must be cleaned */
    long kid = nu(0x1026, child.as, (long)&g_regs, 0, 0, 0);
    KT_CHECK(kid > 0 && kid != tid);
    KT_CHECK(next_req() == 1 && g_req[0] == (uint64_t)kid && g_req[1] == 60 && g_req[2] == 0);
    KT_CHECK(nu(0x1023, kid, 0, 0, 0, 0) == 0);
    KT_CHECK(nu(0x1027, tid, image.as, 0, 0, 0) == 0);       /* execve */
    KT_CHECK(regs(tid, 0) == 0);
    g_regs.rip = CODE_VA;
    KT_CHECK(regs(tid, 1) == 0);
    KT_CHECK(nu(0x1022, tid, 0, 0, 0, 0) == 0);
    KT_CHECK(next_req() == 1 && g_req[0] == (uint64_t)tid && g_req[2] == 0x33);
    KT_CHECK(nu(0x1016, parent.as, 0, 0, 0, 0) == 0);        /* nobody runs in it any more */
    KT_CHECK(nu(0x1016, image.as, 0, 0, 0, 0) == -EBUSY);
    KT_CHECK(nu(0x1023, tid, 0, 0, 0, 0) == 0);
    parent.as = nu(0x1010, 0, 0, 0, 0, 0);                    /* uproc_free destroys a space */
    KT_CHECK(uproc_free(&parent) == 0 && uproc_free(&child) == 0 && uproc_free(&image) == 0);
    return 0;
}
