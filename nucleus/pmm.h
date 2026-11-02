/* pmm.h - physical memory manager core. Pure logic (host-tested).
 *
 * A LIFO free list of 4 KiB frames threaded through the free frames
 * themselves: the first 8 bytes of a free frame hold the physical address
 * of the next one (0 = end). The caller says how to reach a frame
 * (phys_to_ptr: the physmap in the nucleus, a buffer in tests).
 * Not thread-safe: the nucleus wrapper (frames.c) adds the lock. */
#ifndef NUCLEUS_PMM_H
#define NUCLEUS_PMM_H
#include <stdint.h>
#include <burrow/boot_info.h>

#define PMM_FRAME 4096ull

struct pmm_range { uint64_t base, size_bytes; };

struct pmm {
    void    *(*phys_to_ptr)(void *env, uint64_t pa);
    void     *env;
    uint64_t  free_head;      /* 0 = no free frame (frame 0 is never managed) */
    uint64_t  free_frames;
    uint64_t  total_frames;   /* frames ever added */
};

void pmm_init(struct pmm *p, void *(*phys_to_ptr)(void *env, uint64_t pa), void *env);
/* Adds every whole 4 KiB frame inside a usable E820 entry that overlaps no
 * non-usable entry and no reserved range. Frame 0 is never added. */
void pmm_add_e820(struct pmm *p, const struct e820_entry *map, uint32_t count,
                  const struct pmm_range *reserved, uint32_t nreserved);
uint64_t pmm_alloc(struct pmm *p);            /* 0 = out of memory; contents undefined */
void     pmm_free(struct pmm *p, uint64_t pa);  /* pa must come from pmm_alloc */
#endif
