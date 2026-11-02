/* =============================================================================
 * syscall_shim.c - Linux syscall subset that lets the stock Go runtime run
 *                  in ring 0 on the nucleus. (ADR-0003)
 *
 * Design:
 *   - Table-driven: one small function per syscall (SRP). Adding a syscall
 *     = adding a row, never editing the dispatcher (Open/Closed).
 *   - Unknown numbers are LOGGED ONCE with name + args and return -ENOSYS.
 *     This "census on failure" is how you discover what the runtime needs
 *     after a Go upgrade. See docs/01-research/GO_RUNTIME_SYSCALL_CENSUS.md.
 *   - Numbers and errno values are Linux x86-64's, so the same table later
 *     serves musl-based user programs (Wave 9).
 *
 * Census (Go 1.22 host run; re-run for the pinned version!):
 *   arch_prctl clone close epoll_create1 epoll_ctl epoll_pwait fcntl futex
 *   getrlimit gettid madvise mmap nanosleep openat pipe2 read rt_sigaction
 *   rt_sigprocmask sched_getaffinity sched_yield sigaltstack write
 *   (+ exit_group, munmap, tgkill/getpid in some paths)
 * ============================================================================= */
#include <stdint.h>
#include <stddef.h>
#include "nucleus_api.h"

/* Matches the push order in syscall_gate.asm (lowest address first). */
struct syscall_frame {
    uint64_t r15, r14, r13, r12, rbp, rbx;
    uint64_t r9, r8, r10, rdx, rsi, rdi;   /* args are rdi rsi rdx r10 r8 r9 */
    uint64_t nr;                            /* original RAX                   */
    uint64_t gate_ret;                      /* return addr into the gate      */
    uint64_t rflags, rip, rsp;              /* caller state                   */
};
#define A0(f) ((f)->rdi)
#define A1(f) ((f)->rsi)
#define A2(f) ((f)->rdx)
#define A3(f) ((f)->r10)
#define A4(f) ((f)->r8)
#define A5(f) ((f)->r9)

typedef long (*sys_fn)(struct syscall_frame *);

/* ---- Linux x86-64 numbers we implement -------------------------------- */
enum {
    SYS_read = 0, SYS_write = 1, SYS_close = 3, SYS_mmap = 9, SYS_munmap = 11,
    SYS_rt_sigaction = 13, SYS_rt_sigprocmask = 14, SYS_sched_yield = 24,
    SYS_madvise = 28, SYS_nanosleep = 35, SYS_getpid = 39, SYS_clone = 56,
    SYS_fcntl = 72, SYS_getrlimit = 97, SYS_sigaltstack = 131,
    SYS_arch_prctl = 158, SYS_gettid = 186, SYS_futex = 202,
    SYS_sched_getaffinity = 204, SYS_epoll_ctl = 233, SYS_tgkill = 234,
    SYS_exit_group = 231, SYS_openat = 257, SYS_epoll_pwait = 281,
    SYS_epoll_create1 = 291, SYS_pipe2 = 293,
    SYS_MAX = 512,
};

/* ---- trivial ones ------------------------------------------------------ */
static long sys_ok(struct syscall_frame *f)      { (void)f; return 0; }
static long sys_gettid(struct syscall_frame *f)  { (void)f; return thread_id(thread_current()); }
static long sys_getpid(struct syscall_frame *f)  { (void)f; return 1; }
static long sys_yield(struct syscall_frame *f)   { (void)f; thread_yield(); return 0; }
static long sys_enoent(struct syscall_frame *f)  { (void)f; return -ENOENT; }  /* openat: no files yet */

static long sys_write(struct syscall_frame *f) {
    if (A0(f) != 1 && A0(f) != 2) return -EBADF;       /* stdout/stderr -> console */
    console_write((const char *)A1(f), (size_t)A2(f));
    return (long)A2(f);
}

/* Go >= 1.22 checks fds 0,1,2 at start-up and throws if it cannot fix them.
 * We pretend they are open (they are the console). */
static long sys_fcntl(struct syscall_frame *f) {
    return A0(f) <= 2 ? 0 : -EBADF;
}

static long sys_getrlimit(struct syscall_frame *f) {
    uint64_t *rl = (uint64_t *)A1(f);                   /* {cur, max} */
    rl[0] = rl[1] = 1024;
    return 0;
}

static long sys_sched_getaffinity(struct syscall_frame *f) {
    uint8_t *mask = (uint8_t *)A2(f);
    if (A1(f) < 8) return -EINVAL;
    for (int i = 0; i < 8; i++) mask[i] = 0;
    mask[0] = 1;                                        /* one CPU until SMP wave */
    return 8;                                           /* bytes written */
}

/* ---- TLS: the Go runtime keeps its g pointer at -8(FS) --------------- */
#define ARCH_SET_FS 0x1002
static long sys_arch_prctl(struct syscall_frame *f) {
    if (A0(f) != ARCH_SET_FS) return -EINVAL;           /* never let Go touch GS */
    thread_set_fs_base(thread_current(), A1(f));
    return 0;
}

/* ---- memory ------------------------------------------------------------ */
#define PROT_READ 1
#define PROT_WRITE 2
#define PROT_EXEC 4
#define MAP_PRIVATE 0x02
#define MAP_FIXED 0x10
#define MAP_ANONYMOUS 0x20

static long sys_mmap(struct syscall_frame *f) {
    uint64_t hint = A0(f), len = A1(f);
    int prot = (int)A2(f), flags = (int)A3(f);
    if (!(flags & MAP_ANONYMOUS)) return -ENOSYS;       /* files: Wave 8 */
    len = (len + 4095) & ~4095ull;
    if (flags & MAP_FIXED) {                            /* commit inside a reservation */
        long rc = vm_commit(hint, len, prot);
        return rc < 0 ? rc : (long)hint;
    }
    uint64_t addr;
    long rc = vm_reserve(hint, len, &addr);             /* honour hint if free */
    if (rc < 0) return rc;
    if (prot != 0) {                                    /* PROT_NONE = reserve only! */
        rc = vm_commit(addr, len, prot);
        if (rc < 0) { vm_release(addr, len); return rc; }
    }
    return (long)addr;
}
static long sys_munmap(struct syscall_frame *f) { return vm_release(A0(f), A1(f)); }

/* ---- threads ----------------------------------------------------------- */
#define CLONE_VM 0x100
#define CLONE_THREAD 0x10000
#define CLONE_SETTLS 0x80000
static long sys_clone(struct syscall_frame *f) {
    uint64_t flags = A0(f);
    if ((flags & (CLONE_VM | CLONE_THREAD)) != (CLONE_VM | CLONE_THREAD))
        return -ENOSYS;                                 /* fork() comes with processes */
    uint64_t tls = (flags & CLONE_SETTLS) ? A4(f) : 0;
    /* Child resumes at the instruction after SYSCALL, on the new stack, rax=0. */
    return thread_clone(f->rip, A1(f), tls, f->rflags);
}

#define FUTEX_WAIT 0
#define FUTEX_WAKE 1
#define FUTEX_PRIVATE_FLAG 128
static long sys_futex(struct syscall_frame *f) {
    int op = (int)A1(f) & ~FUTEX_PRIVATE_FLAG;
    if (op == FUTEX_WAIT) {
        const int64_t *ts = (const int64_t *)A3(f);     /* struct timespec or NULL */
        int64_t ns = ts ? ts[0] * 1000000000ll + ts[1] : -1;
        return futex_wait((uint32_t *)A0(f), (uint32_t)A2(f), ns);
    }
    if (op == FUTEX_WAKE) return futex_wake((uint32_t *)A0(f), (uint32_t)A2(f));
    return -ENOSYS;
}

static long sys_nanosleep(struct syscall_frame *f) {
    const int64_t *ts = (const int64_t *)A0(f);
    thread_sleep_ns((uint64_t)(ts[0] * 1000000000ll + ts[1]));
    return 0;
}

static long sys_exit_group(struct syscall_frame *f) {
    kprintf("[shim] Go kernel called exit_group(%ld) - this is a kernel panic\n", (long)A0(f));
    thread_exit((int)A0(f));
}

/* epoll/pipe2: the runtime's netpoller. A minimal version only needs one
 * internal "wake pipe" + a timed sleep. Implemented in slice W6-S09. */
long sys_epoll_create1(struct syscall_frame *f);
long sys_epoll_ctl(struct syscall_frame *f);
long sys_epoll_pwait(struct syscall_frame *f);
long sys_pipe2(struct syscall_frame *f);

/* ---- the table ----------------------------------------------------------- */
static const sys_fn table[SYS_MAX] = {
    [SYS_write] = sys_write,           [SYS_close] = sys_ok,
    [SYS_mmap] = sys_mmap,             [SYS_munmap] = sys_munmap,
    [SYS_madvise] = sys_ok,            [SYS_rt_sigaction] = sys_ok,
    [SYS_rt_sigprocmask] = sys_ok,     [SYS_sigaltstack] = sys_ok,
    [SYS_sched_yield] = sys_yield,     [SYS_nanosleep] = sys_nanosleep,
    [SYS_getpid] = sys_getpid,         [SYS_gettid] = sys_gettid,
    [SYS_clone] = sys_clone,           [SYS_fcntl] = sys_fcntl,
    [SYS_getrlimit] = sys_getrlimit,   [SYS_arch_prctl] = sys_arch_prctl,
    [SYS_futex] = sys_futex,           [SYS_sched_getaffinity] = sys_sched_getaffinity,
    [SYS_openat] = sys_enoent,         [SYS_exit_group] = sys_exit_group,
    [SYS_epoll_create1] = sys_epoll_create1, [SYS_epoll_ctl] = sys_epoll_ctl,
    [SYS_epoll_pwait] = sys_epoll_pwait,     [SYS_pipe2] = sys_pipe2,
    /* tgkill: async preemption signal. We boot Go with GODEBUG=asyncpreemptoff=1,
     * so it should never arrive; leaving it out makes the census shout if it does. */
};

static uint8_t reported[SYS_MAX];

long nu_syscall_dispatch(struct syscall_frame *f) {
    uint64_t nr = f->nr;
    if (nr < SYS_MAX && table[nr]) return table[nr](f);
    if (nr >= SYS_MAX || !reported[nr]) {
        if (nr < SYS_MAX) reported[nr] = 1;
        kprintf("[shim] UNIMPLEMENTED syscall %lu rip=%p args=%lx %lx %lx %lx\n",
                (unsigned long)nr, (void *)f->rip, (unsigned long)A0(f),
                (unsigned long)A1(f), (unsigned long)A2(f), (unsigned long)A3(f));
    }
    return -ENOSYS;
}
