/* ktest_user.c - user address spaces and ring 3 (W9), driven the way the
 * Go kernel will drive them: through the nucleus calls. */
#include "ktest.h"
#include "uspace.h"
#include "vmm.h"
#include "frames.h"
#include "boot.h"
#include "errno.h"

#define CODE_VA 0x400000ull

static long nu(long nr, long a0, long a1, long a2, long a3, long a4) {
    long ret;
    register long r10 __asm__("r10") = a3;
    register long r8 __asm__("r8") = a4;
    __asm__ volatile("syscall" : "=a"(ret) : "a"(nr), "D"(a0), "S"(a1), "d"(a2), "r"(r10), "r"(r8)
                     : "rcx", "r11", "memory");
    return ret;
}

/* Map, translate, refuse bad requests, unmap, destroy: every frame comes back. */
int kt_uspace_map_translate(void) {
    uint64_t free0 = frames_free_count();
    long as = nu(0x1010, 0, 0, 0, 0, 0);
    KT_CHECK(as >= 1);
    long pa = nu(0x1014, 1, 0, 0, 0, 0);
    KT_CHECK(pa > 0 && (pa & 0xFFF) == 0);
    KT_CHECK(*(volatile uint64_t *)phys_to_virt((uint64_t)pa) == 0);          /* zeroed */
    KT_CHECK(nu(0x1011, as, CODE_VA, pa, 4096, UPROT_WRITE) == 0);
    KT_CHECK(nu(0x1013, as, CODE_VA + 0x123, 0, 0, 0) == pa + 0x123);
    KT_CHECK(nu(0x1011, as, CODE_VA, pa, 4096, 0) == -EEXIST);
    KT_CHECK(nu(0x1011, as, CODE_VA + 1, pa, 4096, 0) == -EINVAL);           /* unaligned */
    KT_CHECK(nu(0x1011, as, USER_VA_TOP, pa, 4096, 0) == -EINVAL);           /* kernel half */
    KT_CHECK(nu(0x1011, as, CODE_VA, pa, 4096, 8) == -EINVAL);               /* unknown prot */
    KT_CHECK(nu(0x1011, 99, CODE_VA, pa, 4096, 0) == -EBADF);
    KT_CHECK(nu(0x1013, as, 0xFFFFFFFF80100000ll, 0, 0, 0) == -EFAULT);      /* not user */
    KT_CHECK(nu(0x1012, as, CODE_VA, 4096, 0, 0) == 0);
    KT_CHECK(nu(0x1013, as, CODE_VA, 0, 0, 0) == -EFAULT);
    KT_CHECK(nu(0x1016, as, 0, 0, 0, 0) == 0);
    KT_CHECK(nu(0x1016, as, 0, 0, 0, 0) == -EBADF);
    KT_CHECK(nu(0x1015, pa, 1, 0, 0, 0) == 0);
    KT_CHECK(nu(0x1014, 2, 0, 0, 0, 0) == -EINVAL);
    KT_CHECK(frames_free_count() == free0);
    return 0;
}

/* The upper half is the kernel's own (same PDPTs), so kernel stacks and
 * MMIO mapped later are visible from every process; the lower half is empty. */
int kt_uspace_kernel_half_shared(void) {
    long as = nu(0x1010, 0, 0, 0, 0, 0);
    KT_CHECK(as >= 1);
    const uint64_t *user = phys_to_virt(uspace_get((uint64_t)as)->pt.root);
    const uint64_t *kern = phys_to_virt(vmm_kernel()->pt.root);
    for (int i = 0; i < 512; i++)
        KT_CHECK(i < PML4_KERNEL_FIRST ? user[i] == 0 : user[i] == kern[i]);
    uint64_t pa_user, pa_kern, fl;
    uint64_t va = (uint64_t)(uintptr_t)kt_uspace_kernel_half_shared;
    KT_CHECK(vm_translate(uspace_get((uint64_t)as), va, &pa_user, &fl) == PT_OK);
    KT_CHECK(vm_translate(vmm_kernel(), va, &pa_kern, &fl) == PT_OK && pa_user == pa_kern && !(fl & PTE_U));
    KT_CHECK(nu(0x1016, as, 0, 0, 0, 0) == 0);
    return 0;
}
