/* syscall.h - SYSCALL entry (syscall.asm) and the Linux-subset shim (shim.c)
 * the stock Go runtime runs on (ADR-0003, GO_RUNTIME_HOSTING.md §4). */
#ifndef NUCLEUS_SYSCALL_H
#define NUCLEUS_SYSCALL_H
#include <stdint.h>

/* Push order of syscall.asm, lowest address first. */
struct syscall_frame {
    uint64_t r15, r14, r13, r12, rbp, rbx;
    uint64_t r9, r8, r10, rdx, rsi, rdi;   /* Linux args: rdi rsi rdx r10 r8 r9 */
    uint64_t nr;                            /* RAX at entry                      */
    uint64_t rflags, rip, rsp;              /* caller: R11, RCX, RSP             */
};
_Static_assert(sizeof(struct syscall_frame) == 16 * 8, "syscall.asm push count");

#define SYSARG0(f) ((f)->rdi)
#define SYSARG1(f) ((f)->rsi)
#define SYSARG2(f) ((f)->rdx)
#define SYSARG3(f) ((f)->r10)
#define SYSARG4(f) ((f)->r8)
#define SYSARG5(f) ((f)->r9)

/* EFER.SCE, STAR (kernel 0x08, SYSRET base 0x18), LSTAR, FMASK. */
void syscall_init(void);
void syscall_cpu_init(void);        /* the same MSRs on an AP (smp.c) */
/* LSTAR: syscall_entry_user (swapgs first) while a user thread runs on this
 * CPU, syscall_entry_kernel otherwise (ADR-0011); sched.c switches it. */
void syscall_set_entry(int user);
long nu_syscall_dispatch(struct syscall_frame *f);   /* called by syscall.asm only */

/* Go kernel text range, for caller classification (syscall.asm). */
extern uint64_t go_text_lo, go_text_hi;
#endif
