/* uthread.h - user (ring 3) threads and how their system calls and faults
 * reach the Go kernel (W9; NUCLEUS_ABI 0x1020-0x1024, WAVE-09 design).
 *
 * A user thread never runs nucleus policy: each SYSCALL it makes and each
 * exception it takes becomes a request {tid, nr, args[6]} in a FIFO; the
 * thread blocks until Go answers (nu_syscall_reply) or kills it. Go takes
 * requests with nu_syscall_wait, from one dispatcher thread. Must never run
 * Go code on a user CR3, and never trust a user register as an address. */
#ifndef NUCLEUS_UTHREAD_H
#define NUCLEUS_UTHREAD_H
#include <stdint.h>

#define UREQ_WORDS     8u                 /* tid, nr, args[6]: 64 bytes each */
#define UREQ_FAULT     0xFFFF0000ull      /* nr = UREQ_FAULT | vector; args: cr2, rip, error */

struct syscall_frame;
struct trap_frame;

long uthread_create(uint64_t as, uint64_t rip, uint64_t rsp, uint64_t fs);
long uthread_kill(uint64_t tid);
long uthread_set_fs(uint64_t tid, uint64_t fs);
long uthread_wait(uint64_t *buf, uint64_t max);     /* Go's dispatcher */
long uthread_reply(uint64_t tid, long result);

/* syscall.asm's user path: RAX for the return to ring 3. */
long nu_user_syscall(struct syscall_frame *f);
/* trap.c: an exception in ring 3 (never returns), and the check after an
 * interrupt taken in ring 3 (a killed thread leaves instead of returning). */
void uthread_fault(const struct trap_frame *f, uint64_t cr2) __attribute__((noreturn));
void uthread_check_killed(void);
#endif
