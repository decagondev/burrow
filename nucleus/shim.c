/* shim.c - the Linux x86-64 syscall subset the Go runtime needs (census:
 * docs/01-research/GO_RUNTIME_SYSCALL_CENSUS.md, tests/regression/census-go1.27.txt).
 *
 * Table-driven: one small function per syscall (shim_*.c); adding one =
 * adding a row (Open/Closed). Unknown numbers are logged ONCE ("census on
 * failure") and return -ENOSYS: after a Go upgrade, boot and read the log.
 * Numbers and errno values are Linux's, so musl user programs (W9) reuse
 * the table. */
#include "acpi.h"
#include "shim.h"
#include "boot.h"
#include "io.h"
#include "gdt.h"
#include "errno.h"
#include "cpu.h"
#include "kprintf.h"
#include "panic.h"
#include "irq.h"
#include "vmm.h"
#include "uspace.h"
#include "uthread.h"
#include "frames.h"
#include "time.h"
#include "lib_string.h"

#define MSR_STAR   0xC0000081u
#define MSR_LSTAR  0xC0000082u
#define MSR_FMASK  0xC0000084u
#define EFER_SCE   (1ull << 0)
#define FMASK_BITS 0x40700ull          /* AC | DF | IF | TF cleared on entry */

static const sys_fn g_table[SYS_MAX] = {
    [SYS_read] = sys_read,               [SYS_write] = sys_write,
    [SYS_close] = sys_close,             [SYS_getpid] = sys_getpid,
    [SYS_pread64] = sys_pread64,
    [SYS_gettid] = sys_gettid,           [SYS_madvise] = sys_madvise,
    [SYS_rt_sigaction] = sys_rt_sigaction, [SYS_rt_sigprocmask] = sys_rt_sigprocmask,
    [SYS_sigaltstack] = sys_sigaltstack, [SYS_fcntl] = sys_fcntl,
    [SYS_openat] = sys_openat,           [SYS_getrlimit] = sys_getrlimit,
    [SYS_prlimit64] = sys_prlimit64,     [SYS_sched_getaffinity] = sys_sched_getaffinity,
    [SYS_arch_prctl] = sys_arch_prctl,   [SYS_clock_gettime] = sys_clock_gettime,
    [SYS_getrandom] = sys_getrandom,     [SYS_exit_group] = sys_exit_group,
    [SYS_mmap] = sys_mmap,               [SYS_munmap] = sys_munmap,
    [SYS_sched_yield] = sys_sched_yield, [SYS_nanosleep] = sys_nanosleep,
    [SYS_clone] = sys_clone,             [SYS_exit] = sys_exit,
    [SYS_futex] = sys_futex,
    [SYS_eventfd2] = sys_eventfd2,       [SYS_epoll_create1] = sys_epoll_create1,
    [SYS_epoll_ctl] = sys_epoll_ctl,     [SYS_epoll_pwait] = sys_epoll_pwait,
    /* tgkill would be async preemption (SIGURG); GODEBUG=asyncpreemptoff=1
     * means it should never come, so leaving it out makes the census shout. */
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

/* Nucleus-private calls (docs/02-architecture/NUCLEUS_ABI.md), 0x1000-0x1FFF. */
static long nu_physmap_base(struct syscall_frame *f) { (void)f; return (long)boot_info()->physmap_base; }
static long nu_debug_exit(struct syscall_frame *f) {
    kprintf("[shim] nu_debug_exit(%llx)\n", (unsigned long long)SYSARG0(f));
    qemu_exit((uint8_t)SYSARG0(f));    /* ends a QEMU test run; a no-op without the device */
    if (SYSARG0(f) == 0x10) acpi_poweroff();   /* W20-S04: still here? no test device: turn it off */
    return 0;
}
/* Blocks this thread (and only it) until the ISA line fires: ADR-0005. */
static long nu_irq_wait(struct syscall_frame *f) { return irq_wait(SYSARG0(f)); }
static long nu_irq_ack(struct syscall_frame *f)  { return irq_ack(SYSARG0(f)); }
/* boot_info v2 into a Go buffer (same address space, trusted caller). */
static long nu_bootinfo(struct syscall_frame *f) {
    uint64_t n = SYSARG1(f) < sizeof(struct boot_info) ? SYSARG1(f) : sizeof(struct boot_info);
    memcpy((void *)SYSARG0(f), boot_info(), n);
    return (long)n;
}

/* Memory figures for sysinfo(2) (W14-S03): {total, free} bytes of RAM the
 * frame allocator manages. */
static long nu_mem_info(struct syscall_frame *f) {
    uint64_t *out = (uint64_t *)SYSARG0(f);
    out[0] = frames_total_count() * 4096;
    out[1] = frames_free_count() * 4096;
    return 16;
}

/* Wall time from the CMOS clock (W14-S04): unix nanoseconds now. */
static long nu_set_realtime(struct syscall_frame *f) { time_set_realtime_ns(SYSARG0(f)); return 0; }

/* Device memory for a Go driver (a PCI BAR): uncached, NX, at physmap + pa. */
#define MMIO_MAX_BYTES (256ull << 20)
#define PHYS_LIMIT     (1ull << 46)
static long nu_mmio_map(struct syscall_frame *f) {
    uint64_t pa = SYSARG0(f), len = SYSARG1(f);
    if (len == 0 || len > MMIO_MAX_BYTES || pa >= PHYS_LIMIT || len > PHYS_LIMIT - pa) return -EINVAL;
    uint64_t lo = pa & ~0xFFFull, hi = (pa + len + 0xFFF) & ~0xFFFull;
    vm_map_mmio(lo, hi - lo);
    return (long)(boot_info()->physmap_base + pa);
}

/* User address spaces (uspace.c): thin wrappers, the checks live there. */
static long nu_as_create(struct syscall_frame *f)    { (void)f; return uspace_create(); }
static long nu_as_map(struct syscall_frame *f) {
    return uspace_map(SYSARG0(f), SYSARG1(f), SYSARG2(f), SYSARG3(f), SYSARG4(f));
}
static long nu_as_unmap(struct syscall_frame *f)     { return uspace_unmap(SYSARG0(f), SYSARG1(f), SYSARG2(f)); }
static long nu_as_translate(struct syscall_frame *f) { return uspace_translate(SYSARG0(f), SYSARG1(f)); }
static long nu_as_destroy(struct syscall_frame *f)   { return uspace_destroy(SYSARG0(f)); }
static long nu_as_protect(struct syscall_frame *f) {
    return uspace_protect(SYSARG0(f), SYSARG1(f), SYSARG2(f), SYSARG3(f));
}
static long nu_as_map_batch(struct syscall_frame *f) {
    return uspace_map_batch(SYSARG0(f), (const struct uspace_map_entry *)SYSARG1(f), SYSARG2(f));
}
static long nu_page_alloc(struct syscall_frame *f)   { return uspace_page_alloc(SYSARG0(f)); }
static long nu_page_free(struct syscall_frame *f)    { return uspace_page_free(SYSARG0(f), SYSARG1(f)); }

/* User threads and their requests (uthread.c). */
static long nu_uthread_create(struct syscall_frame *f) {
    return uthread_create(SYSARG0(f), SYSARG1(f), SYSARG2(f), SYSARG3(f));
}
static long nu_syscall_wait(struct syscall_frame *f)   { return uthread_wait((uint64_t *)SYSARG0(f), SYSARG1(f)); }
static long nu_syscall_reply(struct syscall_frame *f)  { return uthread_reply(SYSARG0(f), (long)SYSARG1(f)); }
static long nu_uthread_kill(struct syscall_frame *f)   { return uthread_kill(SYSARG0(f)); }
static long nu_uthread_set_fs(struct syscall_frame *f) { return uthread_set_fs(SYSARG0(f), SYSARG1(f)); }
static long nu_uthread_regs(struct syscall_frame *f) {
    return uthread_regs(SYSARG0(f), (struct uregs *)SYSARG1(f), SYSARG2(f) != 0);
}
static long nu_uthread_create_from(struct syscall_frame *f) {
    return uthread_create_from(SYSARG0(f), (const struct uregs *)SYSARG1(f));
}
static long nu_uthread_set_space(struct syscall_frame *f) { return uthread_set_space(SYSARG0(f), SYSARG1(f)); }
static long nu_uthread_interrupt(struct syscall_frame *f) { return uthread_interrupt(SYSARG0(f)); }

static const sys_fn g_nu_table[] = {
    [NU_irq_wait - NU_BASE] = nu_irq_wait,
    [NU_irq_ack - NU_BASE] = nu_irq_ack,
    [NU_bootinfo - NU_BASE] = nu_bootinfo,
    [NU_mmio_map - NU_BASE] = nu_mmio_map,
    [NU_mem_info - NU_BASE] = nu_mem_info,
    [NU_set_realtime - NU_BASE] = nu_set_realtime,
    [NU_physmap_base - NU_BASE] = nu_physmap_base,
    [NU_debug_exit - NU_BASE] = nu_debug_exit,
    [NU_as_create - NU_BASE] = nu_as_create,
    [NU_as_map - NU_BASE] = nu_as_map,
    [NU_as_unmap - NU_BASE] = nu_as_unmap,
    [NU_as_translate - NU_BASE] = nu_as_translate,
    [NU_page_alloc - NU_BASE] = nu_page_alloc,
    [NU_page_free - NU_BASE] = nu_page_free,
    [NU_as_destroy - NU_BASE] = nu_as_destroy,
    [NU_as_protect - NU_BASE] = nu_as_protect,
    [NU_as_map_batch - NU_BASE] = nu_as_map_batch,
    [NU_uthread_create - NU_BASE] = nu_uthread_create,
    [NU_syscall_wait - NU_BASE] = nu_syscall_wait,
    [NU_syscall_reply - NU_BASE] = nu_syscall_reply,
    [NU_uthread_kill - NU_BASE] = nu_uthread_kill,
    [NU_uthread_set_fs - NU_BASE] = nu_uthread_set_fs,
    [NU_uthread_regs - NU_BASE] = nu_uthread_regs,
    [NU_uthread_create_from - NU_BASE] = nu_uthread_create_from,
    [NU_uthread_set_space - NU_BASE] = nu_uthread_set_space,
    [NU_uthread_interrupt - NU_BASE] = nu_uthread_interrupt,
};

long nu_syscall_dispatch(struct syscall_frame *f) {
    uint64_t nr = f->nr;
    if (nr < SYS_MAX && g_table[nr]) return g_table[nr](f);
    if (nr >= NU_BASE && nr - NU_BASE < sizeof g_nu_table / sizeof g_nu_table[0] && g_nu_table[nr - NU_BASE])
        return g_nu_table[nr - NU_BASE](f);
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
    panic("SYSCALL on the kernel CR3 from rip=%llx: neither Go kernel nor nucleus text",
          (unsigned long long)rip);
}

extern void syscall_entry(void);

/* syscall.asm compares CR3 with this: anything else is a user thread. */
uint64_t g_kernel_cr3;

void syscall_init(void) {
    g_kernel_cr3 = vmm_kernel()->pt.root;
    wrmsr(MSR_EFER, rdmsr(MSR_EFER) | EFER_SCE);
    wrmsr(MSR_STAR, ((uint64_t)SEL_UCODE32 << 48) | ((uint64_t)SEL_KCODE << 32));
    wrmsr(MSR_LSTAR, (uint64_t)(uintptr_t)syscall_entry);
    wrmsr(MSR_FMASK, FMASK_BITS);
    kprintf("[nucleus] syscall gate ok (lstar=%p)\n", (void *)syscall_entry);
}
