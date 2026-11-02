/* shim_mm.c - mmap / munmap on the reserve/commit VM (vm_region.h, PIT-05).
 *
 * The Go runtime's patterns (census): PROT_NONE reservations of up to
 * 512 MiB, MAP_FIXED commits inside them, RW anonymous maps, a heap at a
 * hint. Only anonymous private memory exists; files arrive with the VFS
 * (W8). Without demand paging, every committed page is backed (and zeroed)
 * immediately. vm_map_anon does each call as one atomic step: Ms mmap
 * concurrently (BUG-0008). */
#include "shim.h"
#include "vm_region.h"
#include "errno.h"

#define MAP_FIXED           0x10
#define MAP_ANONYMOUS       0x20
#define MAP_FIXED_NOREPLACE 0x100000

long sys_mmap(struct syscall_frame *f) {
    int flags = (int)SYSARG3(f), vm_flags = 0;
    if (!(flags & MAP_ANONYMOUS)) return -ENODEV;
    if (flags & MAP_FIXED) vm_flags |= VM_MAP_FIXED;
    if (flags & MAP_FIXED_NOREPLACE) vm_flags |= VM_MAP_NOREPLACE;
    uint64_t addr;
    long rc = vm_map_anon(vmm_kernel(), SYSARG0(f), SYSARG1(f), (int)SYSARG2(f), vm_flags, &addr);
    return rc ? rc : (long)addr;
}

long sys_munmap(struct syscall_frame *f) {
    if (SYSARG1(f) == 0) return -EINVAL;
    return vm_release(vmm_kernel(), SYSARG0(f), SYSARG1(f));
}

/* MADV_DONTNEED must make the range read as zeros: Go's scavenger relies on
 * it (BUG-0009). Every other advice (NOHUGEPAGE, FREE, ...) is a hint. */
#define MADV_DONTNEED 4
long sys_madvise(struct syscall_frame *f) {
    if (SYSARG2(f) != MADV_DONTNEED) return 0;
    return vm_discard(vmm_kernel(), SYSARG0(f), SYSARG1(f));
}
