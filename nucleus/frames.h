/* frames.h - the nucleus's physical frame allocator (pmm.h core + the real
 * memory map). Frames below 1 MiB and every boot module stay reserved. */
#ifndef NUCLEUS_FRAMES_H
#define NUCLEUS_FRAMES_H
#include <stdint.h>

void     frames_init(void);            /* after boot_init */
uint64_t frame_alloc(void);            /* 0 = out of memory; contents undefined */
uint64_t frame_alloc_zeroed(void);     /* 0 = out of memory */
void     frame_free(uint64_t pa);
uint64_t frames_free_count(void);
uint64_t frames_total_count(void);
/* 1 if [pa, pa+4K) overlaps a range frames_init kept out of the free list. */
int      frame_is_reserved(uint64_t pa);
#endif
