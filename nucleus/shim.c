/* shim.c - the Linux x86-64 syscall subset the Go runtime needs (census:
 * docs/01-research/GO_RUNTIME_SYSCALL_CENSUS.md, tests/regression/census-go1.27.txt).
 *
 * Table-driven: one small function per syscall; adding one = adding a row
 * (Open/Closed). Unknown numbers are logged ONCE ("census on failure") and
 * return -ENOSYS: after a Go upgrade, boot and read the log. Numbers and
 * errno values are Linux's, so musl user programs (W9) reuse the table. */
#include "syscall.h"
#include "sched.h"
#include "gdt.h"
#include "errno.h"
#include "cpu.h"
#include "kprintf.h"
#include "serial.h"
#include "panic.h"

#define MSR_STAR   0xC0000081u
#define MSR_LSTAR  0xC0000082u
#define MSR_FMASK  0xC0000084u
#define EFER_SCE   (1ull << 0)
#define FMASK_BITS 0x40700ull          /* AC | DF | IF | TF cleared on entry */

enum {
    SYS_write = 1, SYS_getpid = 39,
    SYS_MAX = 512,
};

typedef long (*sys_fn)(struct syscall_frame *f);

static long sys_getpid(struct syscall_frame *f) { (void)f; return 1; }

/* fds 1 and 2 are the console (COM1). */
static long sys_write(struct syscall_frame *f) {
    if (SYSARG0(f) != 1 && SYSARG0(f) != 2) return -EBADF;
    const char *buf = (const char *)SYSARG1(f);
    uint64_t n = SYSARG2(f), flags = irq_save();
    for (uint64_t i = 0; i < n; i++) serial_putc(0x3F8, buf[i]);
    irq_restore(flags);
    return (long)n;
}

static const sys_fn g_table[SYS_MAX] = {
    [SYS_write] = sys_write,
    [SYS_getpid] = sys_getpid,
};

static uint8_t g_reported[SYS_MAX];
static uint64_t g_reported_big[32];      /* numbers >= SYS_MAX already logged */
static uint64_t g_reports;

uint64_t shim_unknown_reports(void);
uint64_t shim_unknown_reports(void) { return g_reports; }

/* 1 the first time nr is seen (so it gets logged), 0 after that. */
static int first_report(uint64_t nr) {
    if (nr < SYS_MAX) {
        if (g_reported[nr]) return 0;
        g_reported[nr] = 1;
        return 1;
    }
    for (unsigned i = 0; i < sizeof g_reported_big / sizeof g_reported_big[0]; i++) {
        if (g_reported_big[i] == nr + 1) return 0;          /* stored +1: 0 = empty slot */
        if (g_reported_big[i] == 0) { g_reported_big[i] = nr + 1; return 1; }
    }
    return 1;                                               /* list full: keep logging */
}

long nu_syscall_dispatch(struct syscall_frame *f) {
    uint64_t nr = f->nr;
    if (nr < SYS_MAX && g_table[nr]) return g_table[nr](f);
    if (first_report(nr)) {
        g_reports++;
        kprintf("[shim] UNIMPLEMENTED syscall %llu rip=%llx args=%llx %llx %llx %llx\n",
                (unsigned long long)nr, (unsigned long long)f->rip,
                (unsigned long long)SYSARG0(f), (unsigned long long)SYSARG1(f),
                (unsigned long long)SYSARG2(f), (unsigned long long)SYSARG3(f));
    }
    return -ENOSYS;
}

void syscall_bad_caller(uint64_t rip);   /* syscall.asm */
void syscall_bad_caller(uint64_t rip) {
    panic("SYSCALL from rip=%llx: neither Go kernel nor nucleus text (user mode is W9)",
          (unsigned long long)rip);
}

extern void syscall_entry(void);

void syscall_init(void) {
    wrmsr(MSR_EFER, rdmsr(MSR_EFER) | EFER_SCE);
    wrmsr(MSR_STAR, ((uint64_t)SEL_UCODE32 << 48) | ((uint64_t)SEL_KCODE << 32));
    wrmsr(MSR_LSTAR, (uint64_t)(uintptr_t)syscall_entry);
    wrmsr(MSR_FMASK, FMASK_BITS);
    kprintf("[nucleus] syscall gate ok (lstar=%p)\n", (void *)syscall_entry);
}
