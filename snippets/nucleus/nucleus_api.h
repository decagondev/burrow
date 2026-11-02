/* nucleus_api.h - internal interfaces of the nucleus (C, ring 0, higher half).
 * Each group is one module with one job (SRP). The syscall shim only calls
 * these; it never touches page tables or the scheduler directly (DIP). */
#ifndef NUCLEUS_API_H
#define NUCLEUS_API_H
#include <stdint.h>
#include <stddef.h>

/* ---- console (serial today, framebuffer later) ---- */
void console_write(const char *buf, size_t len);
int  kprintf(const char *fmt, ...);

/* ---- threads & scheduling (nucleus/sched) ---- */
struct thread;
struct thread *thread_current(void);
uint32_t thread_id(struct thread *t);
void     thread_set_fs_base(struct thread *t, uint64_t fs);   /* also wrmsr if current */
/* New thread in the SAME address space, starts at rip with rsp, rax=0. */
long     thread_clone(uint64_t rip, uint64_t rsp, uint64_t tls, uint64_t rflags);
void     thread_yield(void);
void     thread_sleep_ns(uint64_t ns);
void     thread_exit(int code) __attribute__((noreturn));

/* ---- futex wait queues keyed by address (nucleus/sync) ---- */
long futex_wait(uint32_t *addr, uint32_t expected, int64_t timeout_ns); /* 0, -EAGAIN, -ETIMEDOUT */
long futex_wake(uint32_t *addr, uint32_t count);                       /* number woken */

/* ---- virtual memory for the Go kernel's address space (nucleus/vm) ---- */
#define VM_PROT_NONE  0
#define VM_PROT_READ  1
#define VM_PROT_WRITE 2
#define VM_PROT_EXEC  4
/* Reserve address space only. NO physical pages are allocated (see BUG/ADR:
 * the Go runtime reserves >1 GiB of PROT_NONE space at start-up). */
long vm_reserve(uint64_t hint, uint64_t len, uint64_t *out_addr);
/* Back [addr, addr+len) with zeroed pages and set protection. */
long vm_commit(uint64_t addr, uint64_t len, int prot);
long vm_release(uint64_t addr, uint64_t len);

/* ---- time ---- */
uint64_t time_monotonic_ns(void);

/* ---- errno values: identical to Linux, because we speak the Linux ABI ---- */
#define EPERM 1
#define ENOENT 2
#define EINTR 4
#define EBADF 9
#define EAGAIN 11
#define ENOMEM 12
#define EFAULT 14
#define EINVAL 22
#define ENOSYS 38
#define ETIMEDOUT 110
#endif
