/* shim_mm.c - mmap / munmap on the reserve/commit VM (vm_region.h, PIT-05).
 *
 * The Go runtime's patterns (census): PROT_NONE reservations of up to
 * 512 MiB, MAP_FIXED commits inside them, RW anonymous maps, a heap at
 * the 0xc000000000 hint. Only anonymous private memory exists; files
 * arrive with the VFS (W8). Without demand paging, every committed page is
 * backed (and zeroed) immediately. */
#include "shim.h"
#include "vm_region.h"
#include "errno.h"

#define MAP_FIXED           0x10
#define MAP_ANONYMOUS       0x20
#define MAP_FIXED_NOREPLACE 0x100000

static uint64_t page_up(uint64_t v) { return (v + PAGE_4K - 1) & ~(PAGE_4K - 1); }

/* Exactly [addr, addr+len): already reserved, or reservable right there. */
static long reserve_exact(struct addr_space *as, uint64_t addr, uint64_t len, int may_exist) {
    if (regions_covers(&as->regions, addr, len)) return may_exist ? 0 : -EEXIST;
    uint64_t got;
    long rc = vm_reserve(as, addr, len, &got);
    if (rc) return rc;
    if (got != addr) { vm_release(as, got, len); return -ENOMEM; }
    return 0;
}

long sys_mmap(struct syscall_frame *f) {
    uint64_t addr = SYSARG0(f), len = page_up(SYSARG1(f));
    int prot = (int)SYSARG2(f), flags = (int)SYSARG3(f);
    struct addr_space *as = vmm_kernel();
    if (!(flags & MAP_ANONYMOUS)) return -ENODEV;
    if (len == 0 || (addr & (PAGE_4K - 1))) return -EINVAL;

    if (flags & (MAP_FIXED | MAP_FIXED_NOREPLACE)) {
        long rc = reserve_exact(as, addr, len, !(flags & MAP_FIXED_NOREPLACE));
        if (rc) return rc;
        /* A new anonymous mapping reads as zeros: drop old pages, then back. */
        if ((rc = vm_commit(as, addr, len, VM_PROT_NONE)) != 0) return rc;
        if (prot != VM_PROT_NONE && (rc = vm_commit(as, addr, len, prot)) != 0) return rc;
        return (long)addr;
    }
    uint64_t got;
    long rc = vm_reserve(as, addr, len, &got);          /* honours a free hint */
    if (rc) return rc;
    if (prot != VM_PROT_NONE && (rc = vm_commit(as, got, len, prot)) != 0) {
        vm_release(as, got, len);
        return rc;
    }
    return (long)got;
}

long sys_munmap(struct syscall_frame *f) {
    uint64_t addr = SYSARG0(f), len = page_up(SYSARG1(f));
    if (len == 0 || (addr & (PAGE_4K - 1))) return -EINVAL;
    return vm_release(vmm_kernel(), addr, len);
}
