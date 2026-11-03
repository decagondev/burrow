/* uspace.h - user address spaces, the MECHANISM half (W9; NUCLEUS_ABI
 * 0x1010-0x1016). The Go kernel decides what a process's memory holds and
 * asks for it here by handle; the nucleus only builds the page tables.
 * Frames come from nu_page_alloc and stay the caller's: unmap and destroy
 * never free them. Handles are 1..USPACE_MAX; errors are -errno. */
#ifndef NUCLEUS_USPACE_H
#define NUCLEUS_USPACE_H
#include <stdint.h>

#define USPACE_MAX   32u
#define USER_VA_TOP  (1ull << 47)    /* user addresses are below this */
#define UPROT_WRITE  1u
#define UPROT_EXEC   2u
/* nu_page_alloc leaves this many frames for the kernel (W14-S01): a process
 * that asks for the last of RAM gets -ENOMEM, not the Go kernel's next page. */
#define USER_RESERVE_FRAMES ((32ull << 20) / 4096)

long uspace_create(void);
long uspace_map(uint64_t h, uint64_t va, uint64_t pa, uint64_t len, uint64_t prot);
long uspace_unmap(uint64_t h, uint64_t va, uint64_t len);
long uspace_translate(uint64_t h, uint64_t va);   /* pa, or -EFAULT */
long uspace_destroy(uint64_t h);                  /* -EBUSY while threads use it */
long uspace_page_alloc(uint64_t count);           /* count must be 1 */
long uspace_page_free(uint64_t pa, uint64_t count);

struct addr_space;
/* For uthread.c: the address space behind a handle (0 if none) and its
 * thread count, which keeps uspace_destroy away from a live CR3. */
struct addr_space *uspace_get(uint64_t h);
void uspace_thread_ref(uint64_t h, int delta);
#endif
