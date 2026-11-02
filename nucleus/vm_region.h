/* vm_region.h - reserve / commit / release, the three calls the Go runtime's
 * mmap use boils down to (PIT-05, snippets/nucleus/nucleus_api.h):
 *   reserve: claim addresses, no frames (Go reserves > RAM at start-up)
 *   commit:  back pages with zeroed frames and set protection (PROT_NONE
 *            un-backs them again)
 *   release: un-back and forget the addresses
 * Returns 0 or -errno. Lengths are rounded up to whole pages. */
#ifndef NUCLEUS_VM_REGION_H
#define NUCLEUS_VM_REGION_H
#include <stdint.h>
#include "vmm.h"

#define VM_PROT_NONE  0
#define VM_PROT_READ  1
#define VM_PROT_WRITE 2
#define VM_PROT_EXEC  4

/* Lower-half window of the kernel address space and where hint-less
 * reservations start (MEMORY_MAP.md §2: Go runtime reservations). */
#define VM_LO          0x0000000000400000ull
#define VM_HI          0x0000800000000000ull
#define VM_SEARCH_BASE 0x00007D0000000000ull

#define VM_MAP_FIXED     1   /* exactly at addr, replacing (and zeroing) what is there */
#define VM_MAP_NOREPLACE 2   /* exactly at addr, -EEXIST if already reserved */

void vm_region_init(struct addr_space *as);
long vm_reserve(struct addr_space *as, uint64_t hint, uint64_t len, uint64_t *out);
long vm_commit(struct addr_space *as, uint64_t addr, uint64_t len, int prot);
long vm_release(struct addr_space *as, uint64_t addr, uint64_t len);
/* mmap(MAP_ANONYMOUS) in one atomic step: reserve (hint or exact) and, unless
 * prot is PROT_NONE, back with zeroed pages. *out = the address. */
long vm_map_anon(struct addr_space *as, uint64_t addr, uint64_t len, int prot, int flags, uint64_t *out);
/* madvise(MADV_DONTNEED) on private anonymous memory: the pages stay
 * mapped and read as zeros afterwards. Go 1.27 RELIES on this: it hands out
 * scavenged spans without clearing them (mheap.go initSpan, BUG-0009).
 * Without demand paging the frames stay committed. */
long vm_discard(struct addr_space *as, uint64_t addr, uint64_t len);
#endif
