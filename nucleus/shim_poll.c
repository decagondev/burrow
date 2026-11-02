/* shim_poll.c - just enough of eventfd + epoll for the Go netpoller, which
 * is how the runtime sleeps until the next timer (and is woken early by
 * netpollBreak, an 8-byte write to its eventfd).
 *
 * A tiny fd table: 0-2 are the console (shim_proc.c), 3.. hold eventfds and
 * epoll instances. epoll_pwait reports registered eventfds whose counter is
 * non-zero; otherwise it sleeps on g_poll_seq, which every eventfd write
 * bumps. The sequence is read BEFORE the readiness check, so a write that
 * lands in between makes futex_wait return at once (no lost wake-up). */
#include "shim.h"
#include "futex.h"
#include "spinlock.h"
#include "errno.h"
#include "lib_string.h"

#define FD_FIRST    3
#define FD_MAX      16
#define WATCH_MAX   8
#define EPOLLIN     0x001
#define EPOLL_CTL_ADD 1
#define EPOLL_CTL_DEL 2
#define EPOLL_CTL_MOD 3

enum fd_kind { FD_NONE = 0, FD_EVENTFD, FD_EPOLL };

struct epoll_event { uint32_t events; uint64_t data; } __attribute__((packed));   /* x86-64 ABI */
struct watch { int fd; uint32_t events; uint64_t data; };

struct file {
    enum fd_kind kind;
    uint64_t counter;                 /* eventfd */
    struct watch w[WATCH_MAX];        /* epoll: fd < 0 = free slot */
};

static struct file g_files[FD_MAX];
static struct spinlock g_lock = SPINLOCK_INIT("poll");
static uint32_t g_poll_seq;

static struct file *get(int fd, enum fd_kind kind) {
    return fd >= FD_FIRST && fd < FD_MAX && g_files[fd].kind == kind ? &g_files[fd] : 0;
}

static long new_fd(enum fd_kind kind) {
    uint64_t fl = spin_lock_irqsave(&g_lock);
    for (int fd = FD_FIRST; fd < FD_MAX; fd++) {
        if (g_files[fd].kind != FD_NONE) continue;
        memset(&g_files[fd], 0, sizeof g_files[fd]);
        g_files[fd].kind = kind;
        for (int i = 0; i < WATCH_MAX; i++) g_files[fd].w[i].fd = -1;
        spin_unlock_irqrestore(&g_lock, fl);
        return fd;
    }
    spin_unlock_irqrestore(&g_lock, fl);
    return -EMFILE;
}

long sys_eventfd2(struct syscall_frame *f) {
    long fd = new_fd(FD_EVENTFD);
    if (fd >= 0) g_files[fd].counter = SYSARG0(f);
    return fd;
}

long sys_epoll_create1(struct syscall_frame *f) { (void)f; return new_fd(FD_EPOLL); }

long sys_epoll_ctl(struct syscall_frame *f) {
    struct file *ep = get((int)SYSARG0(f), FD_EPOLL);
    int op = (int)SYSARG1(f), fd = (int)SYSARG2(f);
    const struct epoll_event *ev = (const struct epoll_event *)SYSARG3(f);
    if (!ep) return -EBADF;
    uint64_t fl = spin_lock_irqsave(&g_lock);
    long rc = op == EPOLL_CTL_ADD ? -ENOSPC : -ENOENT;
    for (int i = 0; i < WATCH_MAX; i++) {
        struct watch *w = &ep->w[i];
        if (op == EPOLL_CTL_ADD && w->fd < 0) { *w = (struct watch){ fd, ev->events, ev->data }; rc = 0; break; }
        if (op != EPOLL_CTL_ADD && w->fd == fd) {
            if (op == EPOLL_CTL_DEL) w->fd = -1;
            else { w->events = ev->events; w->data = ev->data; }
            rc = 0;
            break;
        }
    }
    spin_unlock_irqrestore(&g_lock, fl);
    return rc;
}

/* Ready watches -> out[], at most max. Caller holds g_lock. */
static int collect(const struct file *ep, struct epoll_event *out, int max) {
    int n = 0;
    for (int i = 0; i < WATCH_MAX && n < max; i++) {
        const struct watch *w = &ep->w[i];
        const struct file *src = w->fd >= 0 ? get(w->fd, FD_EVENTFD) : 0;
        if (src && src->counter && (w->events & EPOLLIN)) out[n++] = (struct epoll_event){ EPOLLIN, w->data };
    }
    return n;
}

/* epoll_pwait(epfd, events, maxevents, timeout_ms, sigmask). May return 0
 * early (spurious); the Go netpoller simply loops. */
long sys_epoll_pwait(struct syscall_frame *f) {
    struct file *ep = get((int)SYSARG0(f), FD_EPOLL);
    struct epoll_event *out = (struct epoll_event *)SYSARG1(f);
    int max = (int)SYSARG2(f), timeout_ms = (int)SYSARG3(f);
    if (!ep || max <= 0) return ep ? -EINVAL : -EBADF;
    for (int round = 0; round < 2; round++) {
        uint32_t seq = __atomic_load_n(&g_poll_seq, __ATOMIC_ACQUIRE);
        uint64_t fl = spin_lock_irqsave(&g_lock);
        int n = collect(ep, out, max);
        spin_unlock_irqrestore(&g_lock, fl);
        if (n || timeout_ms == 0 || round == 1) return n;
        futex_wait(&g_poll_seq, seq, timeout_ms < 0 ? 0 : (uint64_t)timeout_ms * 1000000ull);
    }
    return 0;
}

/* read/write/close on fds >= 3 (shim_proc.c forwards them here). */
long poll_read(int fd, void *buf, uint64_t n) {
    struct file *e = get(fd, FD_EVENTFD);
    if (!e) return -EBADF;
    if (n < 8) return -EINVAL;
    uint64_t fl = spin_lock_irqsave(&g_lock);
    uint64_t v = e->counter;
    e->counter = 0;
    spin_unlock_irqrestore(&g_lock, fl);
    if (!v) return -EAGAIN;                        /* Go opens it EFD_NONBLOCK */
    memcpy(buf, &v, 8);
    return 8;
}

long poll_write(int fd, const void *buf, uint64_t n) {
    struct file *e = get(fd, FD_EVENTFD);
    if (!e) return -EBADF;
    if (n < 8) return -EINVAL;
    uint64_t v;
    memcpy(&v, buf, 8);
    uint64_t fl = spin_lock_irqsave(&g_lock);
    e->counter += v;
    spin_unlock_irqrestore(&g_lock, fl);
    __atomic_add_fetch(&g_poll_seq, 1, __ATOMIC_RELEASE);
    futex_wake(&g_poll_seq, 0xFFFFFFFFu);
    return 8;
}

long poll_close(int fd) {
    if (fd < FD_FIRST || fd >= FD_MAX || g_files[fd].kind == FD_NONE) return -EBADF;
    g_files[fd].kind = FD_NONE;
    return 0;
}
