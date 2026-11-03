/* ktest_syscall.c - the SYSCALL gate and the shim, called from nucleus code
 * (ring 0, like the Go kernel). */
#include "ktest.h"
#include "syscall.h"
#include "errno.h"
#include "cpu.h"
#include "boot.h"
#include "apic.h"
#include "frames.h"
#include "trap.h"
#include <burrow/boot_info.h>

extern uint64_t ktest_syscall_clobbers(void);
extern uint64_t shim_unknown_reports(void);

static long sys3(long nr, long a0, long a1, long a2) {
    long ret;
    register long r10 __asm__("r10") = 0;
    __asm__ volatile("syscall" : "=a"(ret) : "a"(nr), "D"(a0), "S"(a1), "d"(a2), "r"(r10)
                     : "rcx", "r11", "memory");
    return ret;
}

int kt_syscall_getpid(void) {
    uint64_t if_before = read_rflags() & RFLAGS_IF;
    KT_CHECK(sys3(39, 0, 0, 0) == 1);
    KT_CHECK((read_rflags() & RFLAGS_IF) == if_before);    /* IRETQ restored IF */
    KT_CHECK(ktest_syscall_clobbers() == 0);                /* only RAX, RCX, R11 change */
    return 0;
}

int kt_syscall_write(void) {
    static const char msg[] = "[ktest] hello through write(1)\n";
    KT_CHECK(sys3(1, 1, (long)msg, sizeof msg - 1) == (long)(sizeof msg - 1));
    KT_CHECK(sys3(1, 7, (long)msg, 1) == -EBADF);
    return 0;
}

static long sys6(long nr, long a0, long a1, long a2, long a3, long a4, long a5) {
    long ret;
    register long r10 __asm__("r10") = a3;
    register long r8 __asm__("r8") = a4;
    register long r9 __asm__("r9") = a5;
    __asm__ volatile("syscall" : "=a"(ret) : "a"(nr), "D"(a0), "S"(a1), "d"(a2), "r"(r10), "r"(r8), "r"(r9)
                     : "rcx", "r11", "memory");
    return ret;
}

/* BUG-0009: after madvise(MADV_DONTNEED) anonymous memory must read as zeros
 * (Go 1.27 hands out scavenged spans without clearing them). */
int kt_madvise_dontneed_zeroes(void) {
    long addr = sys6(9, 0, 3 * 4096, 3 /* RW */, 0x22 /* PRIVATE|ANON */, -1, 0);
    KT_CHECK(addr > 0);
    uint64_t *p = (uint64_t *)addr;
    p[0] = 0x1111; p[512] = 0x2222; p[1024] = 0x3333;          /* one word per page */
    KT_CHECK(sys3(28, addr + 4096, 4096, 4 /* MADV_DONTNEED */) == 0);
    KT_CHECK(p[0] == 0x1111 && p[512] == 0 && p[1024] == 0x3333);   /* only the middle page */
    p[512] = 0x4444;                                           /* still mapped and writable */
    KT_CHECK(sys3(28, addr, 3 * 4096, 15 /* MADV_NOHUGEPAGE: a hint */) == 0);
    KT_CHECK(p[0] == 0x1111 && p[512] == 0x4444);
    KT_CHECK(sys3(11, addr, 3 * 4096, 0) == 0);                 /* munmap */
    return 0;
}

/* BUG-0012: MADV_DONTNEED hands the frames back; touching a page faults a
 * zeroed one in; a page turned PROT_NONE after a discard really faults. */
int kt_madvise_returns_frames(void) {
    const long pages = 64;
    long addr = sys6(9, 0, pages * 4096, 3, 0x22, -1, 0);
    KT_CHECK(addr > 0);
    volatile uint64_t *p = (volatile uint64_t *)addr;
    for (long i = 0; i < pages; i++) p[i * 512] = 0xAB00 + (uint64_t)i;
    uint64_t before = frames_free_count();
    KT_CHECK(sys3(28, addr, pages * 4096, 4) == 0);
    KT_CHECK(frames_free_count() == before + (uint64_t)pages);
    KT_CHECK(p[5 * 512] == 0);                                 /* faults one zeroed page in */
    KT_CHECK(frames_free_count() == before + (uint64_t)pages - 1);
    p[5 * 512] = 7;
    KT_CHECK(p[5 * 512] == 7);
    KT_CHECK(sys6(9, addr + 4096, 4096, 0 /* PROT_NONE */, 0x32 /* FIXED */, -1, 0) == addr + 4096);
    uint64_t v;
    KT_CHECK(probe_read64((uint64_t)addr + 4096, &v) == -1 && trap_last.vector == VEC_PF);
    KT_CHECK(sys3(11, addr, pages * 4096, 0) == 0);
    return 0;
}

/* "Census on failure": an unknown number is logged once and returns -ENOSYS. */
int kt_shim_unknown_once(void) {
    uint64_t before = shim_unknown_reports();
    KT_CHECK(sys3(999, 1, 2, 3) == -ENOSYS);
    KT_CHECK(sys3(999, 1, 2, 3) == -ENOSYS);
    KT_CHECK(shim_unknown_reports() == before + 1);
    return 0;
}

/* W14-S03: nu_mem_info reports the frame allocator's totals in bytes, and
 * the free figure follows an allocation. */
int kt_nu_mem_info(void) {
    uint64_t m[2];
    KT_CHECK(sys3(0x1006, (long)m, 0, 0) == 16);
    KT_CHECK(m[0] == frames_total_count() * 4096 && m[1] == frames_free_count() * 4096);
    KT_CHECK(m[1] > 0 && m[1] <= m[0]);
    uint64_t pa = frame_alloc();
    KT_CHECK(pa != 0);
    uint64_t n[2];
    sys3(0x1006, (long)n, 0, 0);
    frame_free(pa);
    KT_CHECK(n[0] == m[0] && n[1] == m[1] - 4096);
    return 0;
}

/* W8-S01: nu_bootinfo copies boot_info v2; nu_mmio_map maps device memory
 * at physmap + pa (the LAPIC page: its ID register reads back) and rejects
 * bad ranges instead of panicking. */
int kt_nu_bootinfo_mmio(void) {
    uint8_t buf[256];
    KT_CHECK(sys3(0x1002, (long)buf, sizeof buf, 0) == (long)sizeof(struct boot_info));
    const struct boot_info *bi = (const struct boot_info *)buf;
    KT_CHECK(bi->magic == BOOTINFO_MAGIC && bi->version == BOOTINFO_VERSION);
    KT_CHECK(sys3(0x1002, (long)buf, 8, 0) == 8);

    const uint64_t lapic = 0xFEE00000ull;
    long va = sys3(0x1005, (long)(lapic + 0x20), 4, 0);
    KT_CHECK(va == (long)(boot_info()->physmap_base + lapic + 0x20));
    KT_CHECK(*(volatile uint32_t *)va == lapic_read(LAPIC_ID));
    KT_CHECK(sys3(0x1005, (long)lapic, 0, 0) == -EINVAL);
    KT_CHECK(sys3(0x1005, 1ll << 46, 4096, 0) == -EINVAL);
    KT_CHECK(sys3(0x1005, 0, (512ll << 20), 0) == -EINVAL);
    return 0;
}
