/* shim.h - internal to the syscall shim: Linux x86-64 numbers and the
 * handlers, grouped by topic (shim_proc.c, shim_mm.c, shim_thread.c,
 * shim_poll.c). The table that maps one to the other lives in shim.c. */
#ifndef NUCLEUS_SHIM_H
#define NUCLEUS_SHIM_H
#include "syscall.h"

enum {
    SYS_read = 0, SYS_write = 1, SYS_close = 3, SYS_mmap = 9, SYS_munmap = 11,
    SYS_rt_sigaction = 13, SYS_rt_sigprocmask = 14, SYS_pread64 = 17,
    SYS_sched_yield = 24, SYS_madvise = 28, SYS_nanosleep = 35, SYS_getpid = 39,
    SYS_clone = 56, SYS_exit = 60, SYS_fcntl = 72, SYS_getrlimit = 97,
    SYS_sigaltstack = 131, SYS_arch_prctl = 158, SYS_gettid = 186, SYS_futex = 202,
    SYS_sched_getaffinity = 204, SYS_clock_gettime = 228, SYS_exit_group = 231,
    SYS_epoll_ctl = 233, SYS_tgkill = 234, SYS_openat = 257, SYS_epoll_pwait = 281,
    SYS_eventfd2 = 290, SYS_epoll_create1 = 291, SYS_pipe2 = 293, SYS_prlimit64 = 302,
    SYS_getrandom = 318,
    SYS_MAX = 512,
};

/* Nucleus-private calls, NUCLEUS_ABI.md (numbers never reused). */
enum { NU_BASE = 0x1000, NU_irq_wait = 0x1000, NU_irq_ack = 0x1001,
       NU_bootinfo = 0x1002, NU_physmap_base = 0x1003, NU_debug_exit = 0x1004,
       NU_mmio_map = 0x1005,
       NU_as_create = 0x1010, NU_as_map = 0x1011, NU_as_unmap = 0x1012, NU_as_translate = 0x1013,
       NU_page_alloc = 0x1014, NU_page_free = 0x1015, NU_as_destroy = 0x1016 };

typedef long (*sys_fn)(struct syscall_frame *f);

/* shim_proc.c: identity, signals (accepted, never delivered), fds, time */
long sys_write(struct syscall_frame *f);
long sys_read(struct syscall_frame *f);
long sys_close(struct syscall_frame *f);
long sys_pread64(struct syscall_frame *f);
long sys_getpid(struct syscall_frame *f);
long sys_gettid(struct syscall_frame *f);
long sys_rt_sigaction(struct syscall_frame *f);
long sys_rt_sigprocmask(struct syscall_frame *f);
long sys_sigaltstack(struct syscall_frame *f);
long sys_fcntl(struct syscall_frame *f);
long sys_openat(struct syscall_frame *f);
long sys_getrlimit(struct syscall_frame *f);
long sys_prlimit64(struct syscall_frame *f);
long sys_sched_getaffinity(struct syscall_frame *f);
long sys_arch_prctl(struct syscall_frame *f);
long sys_clock_gettime(struct syscall_frame *f);
long sys_getrandom(struct syscall_frame *f);
long sys_exit_group(struct syscall_frame *f);

/* shim_mm.c: anonymous memory on reserve/commit */
long sys_mmap(struct syscall_frame *f);
long sys_munmap(struct syscall_frame *f);
long sys_madvise(struct syscall_frame *f);

/* shim_thread.c: threads, sleep, futex */
long sys_clone(struct syscall_frame *f);
long sys_exit(struct syscall_frame *f);
long sys_futex(struct syscall_frame *f);
long sys_sched_yield(struct syscall_frame *f);
long sys_nanosleep(struct syscall_frame *f);

/* shim_poll.c: eventfd + epoll for the netpoller, and fds >= 3 */
long sys_eventfd2(struct syscall_frame *f);
long sys_epoll_create1(struct syscall_frame *f);
long sys_epoll_ctl(struct syscall_frame *f);
long sys_epoll_pwait(struct syscall_frame *f);
long poll_read(int fd, void *buf, uint64_t n);
long poll_write(int fd, const void *buf, uint64_t n);
long poll_close(int fd);
#endif
