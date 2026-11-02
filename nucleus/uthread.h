/* uthread.h - user (ring 3) threads and how their system calls and faults
 * reach the Go kernel (W9-W10; NUCLEUS_ABI 0x1020-0x1028, WAVE-09/10).
 *
 * A user thread never runs nucleus policy: each SYSCALL it makes, each
 * exception it takes, and each asynchronous stop Go asks for becomes a
 * request {tid, nr, args[6]} in a FIFO; the thread blocks with its whole
 * register state (a struct trap_frame on its kernel stack, plus its FXSAVE
 * area) until Go answers (nu_syscall_reply: it resumes, perhaps with new
 * registers) or kills it. Go takes requests with nu_syscall_wait. Must never
 * run Go code on a user CR3, and never trust a user register as an address. */
#ifndef NUCLEUS_UTHREAD_H
#define NUCLEUS_UTHREAD_H
#include <stdint.h>

#define UREQ_WORDS     8u                 /* tid, nr, args[6]: 64 bytes each */
#define UREQ_FAULT     0xFFFF0000ull      /* nr = UREQ_FAULT | vector; args: cr2, rip, error */
#define UREQ_INTERRUPT 0xFFFF0100ull      /* stopped by nu_uthread_interrupt; args: rip */

/* The register image of nu_uthread_regs / nu_uthread_create_from. */
struct uregs {
    uint64_t r15, r14, r13, r12, r11, r10, r9, r8, rbp, rdi, rsi, rdx, rcx, rbx, rax;
    uint64_t rip, rflags, rsp, fs_base, pad;
    uint8_t  fx[512];                     /* FXSAVE layout */
};
_Static_assert(sizeof(struct uregs) == 672, "NUCLEUS_ABI nu_uthread_regs");

struct trap_frame;

long uthread_create(uint64_t as, uint64_t rip, uint64_t rsp, uint64_t fs);
long uthread_kill(uint64_t tid);
long uthread_set_fs(uint64_t tid, uint64_t fs);
long uthread_wait(uint64_t *buf, uint64_t max);     /* Go's dispatcher */
long uthread_reply(uint64_t tid, long result);

/* uregs.c: registers of a thread whose request Go holds; fork; execve; stops. */
long uthread_regs(uint64_t tid, struct uregs *buf, int write);
long uthread_create_from(uint64_t as, const struct uregs *buf);
long uthread_set_space(uint64_t tid, uint64_t as);
long uthread_interrupt(uint64_t tid);          /* uthread.c */
/* For uregs.c: the thread with this id if Go holds its request (lock held). */
struct thread;
struct thread *uthread_held(uint64_t tid, uint64_t *flags);
void uthread_unlock(uint64_t flags);

/* syscall.asm's user path: RAX of the frame becomes the result. */
void nu_user_syscall(struct trap_frame *f);
/* trap.c: an exception in ring 3 (returns to resume it), and the check on
 * every return to ring 3 (a killed thread leaves, an interrupted one stops). */
void uthread_fault(struct trap_frame *f, uint64_t cr2);
void uthread_return_to_user(struct trap_frame *f);
#endif
