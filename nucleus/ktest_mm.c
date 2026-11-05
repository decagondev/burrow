/* ktest_mm.c - physical and virtual memory on the real machine. */
#include "ktest.h"
#include "frames.h"
#include "boot.h"
#include "vmm.h"
#include "trap.h"
#include "vm_region.h"
#include "errno.h"

#define TEST_VA 0x0000400000200000ull   /* lower half, unused until W6 */

/* Take every free frame, check none is reserved and each is writable
 * through the physmap, give them all back: the counts must match. */
int kt_pmm_alloc_all_free_all(void) {
    uint64_t before = frames_free_count();
    KT_CHECK(before > 1000);
    uint64_t head = 0, n = 0, pa;
    while ((pa = frame_alloc()) != 0) {
        KT_CHECK(!frame_is_reserved(pa));
        uint64_t *p = phys_to_virt(pa);
        p[0] = head;                         /* chain them through themselves */
        p[1] = ~pa;
        head = pa;
        n++;
    }
    KT_CHECK(n == before);
    KT_CHECK(frames_free_count() == 0);
    while (head) {
        uint64_t *p = phys_to_virt(head);
        KT_CHECK(p[1] == ~head);             /* nobody else wrote into it */
        uint64_t next = p[0];
        frame_free(head);
        head = next;
    }
    KT_CHECK(frames_free_count() == before);
    return 0;
}

/* Map a fresh frame, write through the new address, read it back through
 * the physmap, translate, unmap (freeing the frame): the address must then
 * fault with #PF, CR2 = the address, and the frame count must be back. */
int kt_vmm_map_write_unmap(void) {
    struct addr_space *as = vmm_kernel();
    uint64_t free_before = frames_free_count();
    uint64_t pa = frame_alloc_zeroed();
    KT_CHECK(pa != 0);
    KT_CHECK(vm_map(as, TEST_VA, pa, PAGE_4K, PTE_W | PTE_NX) == PT_OK);
    KT_CHECK(probe_write64(TEST_VA + 8, 0x1234abcd) == 0);
    KT_CHECK(((uint64_t *)phys_to_virt(pa))[1] == 0x1234abcd);
    uint64_t got, fl;
    KT_CHECK(vm_translate(as, TEST_VA + 8, &got, &fl) == PT_OK);
    KT_CHECK(got == pa + 8);
    KT_CHECK(vm_unmap(as, TEST_VA, PAGE_4K, 1) == PT_OK);
    uint64_t v;
    KT_CHECK(probe_read64(TEST_VA + 8, &v) == -1);
    KT_CHECK(trap_last.vector == VEC_PF && trap_last.cr2 == TEST_VA + 8);
    /* frames used for new page tables stay allocated; the data frame is back */
    KT_CHECK(frames_free_count() >= free_before - 3 && frames_free_count() <= free_before);
    return 0;
}

/* Read-only must hold in ring 0 (CR0.WP): a write faults with P=1, W=1. */
int kt_vmm_protect_readonly(void) {
    struct addr_space *as = vmm_kernel();
    uint64_t pa = frame_alloc_zeroed();
    KT_CHECK(pa != 0);
    KT_CHECK(vm_map(as, TEST_VA, pa, PAGE_4K, PTE_W | PTE_NX) == PT_OK);
    KT_CHECK(probe_write64(TEST_VA, 1) == 0);
    KT_CHECK(vm_protect(as, TEST_VA, PAGE_4K, PTE_NX) == PT_OK);
    KT_CHECK(probe_write64(TEST_VA, 2) == -1);
    KT_CHECK(trap_last.vector == VEC_PF && (trap_last.error & 3) == 3);
    uint64_t v = 0;
    KT_CHECK(probe_read64(TEST_VA, &v) == 0 && v == 1);       /* still readable, old value */
    KT_CHECK(vm_unmap(as, TEST_VA, PAGE_4K, 1) == PT_OK);
    return 0;
}

/* The nucleus's own text is read-only, and the identity map is gone. */
int kt_vmm_kernel_wx(void) {
    extern char nucleus_entry[];
    uint64_t text = (uint64_t)(uintptr_t)nucleus_entry;
    KT_CHECK(probe_write64(text, 0) == -1);
    KT_CHECK(trap_last.vector == VEC_PF && (trap_last.error & 1));
    uint64_t v;
    KT_CHECK(probe_read64(0x1000, &v) == -1);                 /* boot_info's old identity address */
    KT_CHECK(trap_last.vector == VEC_PF && trap_last.cr2 == 0x1000);
    return 0;
}

/* PIT-05: reserving 2 GiB on a 128 MiB machine must cost no frames.
 * BUG-0012: committing costs page tables only (at most 3); a page's frame
 * comes with its first touch, zeroed. */
int kt_vm_reserve_commit(void) {
    struct addr_space *as = vmm_kernel();
    uint64_t free0 = frames_free_count(), base;
    KT_CHECK(vm_reserve(as, 0, 2ull << 30, &base) == 0);
    KT_CHECK(base == VM_SEARCH_BASE);
    KT_CHECK(frames_free_count() == free0);
    uint64_t v;
    KT_CHECK(probe_read64(base, &v) == -1);                   /* reserved is not backed */

    KT_CHECK(vm_commit(as, base, 100, VM_PROT_READ | VM_PROT_WRITE) == 0);   /* rounds to 1 page */
    uint64_t tables = free0 - frames_free_count();
    KT_CHECK(tables <= 3);                                    /* no data frame yet */
    KT_CHECK(probe_read64(base, &v) == 0 && v == 0);          /* the first touch backs it, zeroed */
    KT_CHECK(free0 - frames_free_count() == tables + 1);
    KT_CHECK(probe_write64(base + 8, 42) == 0);

    uint64_t free1 = frames_free_count();
    KT_CHECK(vm_commit(as, base + PAGE_4K, PAGE_4K, VM_PROT_READ | VM_PROT_WRITE) == 0);
    KT_CHECK(frames_free_count() == free1);                   /* same tables, nothing touched */
    KT_CHECK(probe_write64(base + PAGE_4K, 1) == 0);
    KT_CHECK(free1 - frames_free_count() == 1);               /* exactly one frame */

    KT_CHECK(vm_commit(as, base, PAGE_4K, VM_PROT_READ) == 0);           /* re-protect keeps data */
    KT_CHECK(probe_read64(base + 8, &v) == 0 && v == 42);
    KT_CHECK(probe_write64(base + 8, 1) == -1);

    KT_CHECK(vm_commit(as, base - PAGE_4K, PAGE_4K, VM_PROT_READ) == -EINVAL);  /* not reserved */
    KT_CHECK(vm_release(as, base, 2ull << 30) == 0);
    KT_CHECK(frames_free_count() == free0 - tables);          /* data frames back, tables kept */
    KT_CHECK(vm_commit(as, base, PAGE_4K, VM_PROT_READ) == -EINVAL);
    return 0;
}

/* The Go runtime asks for its heap at 0xc000000000: a free hint is honoured. */
int kt_vm_reserve_hint(void) {
    struct addr_space *as = vmm_kernel();
    uint64_t got;
    KT_CHECK(vm_reserve(as, 0xc000000000ull, 64ull << 20, &got) == 0);
    KT_CHECK(got == 0xc000000000ull);
    uint64_t again;
    KT_CHECK(vm_reserve(as, 0xc000000000ull, PAGE_4K, &again) == 0);
    KT_CHECK(again != got);                                   /* taken: placed elsewhere */
    KT_CHECK(vm_release(as, got, 64ull << 20) == 0);
    KT_CHECK(vm_release(as, again, PAGE_4K) == 0);
    return 0;
}

/* BUG-0027: two CPUs fault on one demand-zero page of the Go kernel; the
 * first fills it, so the second finds it present, not marked. That fault is
 * answered (resume and retry), never reported as a bad pointer. */
int kt_bug0027_fault_in_present(void) {
    struct addr_space *k = vmm_kernel();
    uint64_t pa = frame_alloc_zeroed();
    KT_CHECK(pa && vm_map(k, TEST_VA, pa, PAGE_4K, PTE_W | PTE_NX) == PT_OK);
    KT_CHECK(vm_fault_in(k, TEST_VA + 8) == 1);        /* present: the other CPU won */
    KT_CHECK(vm_unmap(k, TEST_VA, PAGE_4K, 1) == PT_OK);
    KT_CHECK(vm_fault_in(k, TEST_VA) == 0);            /* absent and unmarked: a real fault */
    return 0;
}
