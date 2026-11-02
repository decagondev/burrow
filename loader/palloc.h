/* palloc.h - the loader's physical frame allocator. Pure logic.
 *
 * A bump allocator over the E820 map: hands out 4 KiB frames (optionally
 * several contiguous ones) inside usable RAM, between `floor` and `limit`,
 * never inside a non-usable E820 entry or a reserved range. Frames are never
 * freed; the loader reports [first, end) to the nucleus as "in use".
 * An allocation never spans two E820 entries. Returns 0 on failure, so
 * `floor` must be above 0 (it is 1 MiB in the loader). */
#ifndef LOADER_PALLOC_H
#define LOADER_PALLOC_H
#include <stdint.h>
#include <burrow/boot_info.h>

#define PAGE_SIZE           4096u
#define PALLOC_MAX_RESERVED 8u

struct phys_range { uint64_t base, size_bytes; };

struct palloc {
    const struct e820_entry *map;
    uint32_t count;
    uint32_t nreserved;
    struct phys_range reserved[PALLOC_MAX_RESERVED];
    uint64_t floor, limit;   /* only hand out frames inside [floor, limit)    */
    uint64_t next;           /* bump cursor                                   */
    uint64_t first, end;     /* span actually handed out (first == 0: none)   */
};

void     palloc_init(struct palloc *p, const struct e820_entry *map, uint32_t count,
                     uint64_t floor, uint64_t limit);
/* Never hand out anything overlapping [base, base+size_bytes). 0, or -1 if full. */
int      palloc_reserve(struct palloc *p, uint64_t base, uint64_t size_bytes);
/* count_frames contiguous frames (contents NOT zeroed). 0 = out of memory. */
uint64_t palloc_alloc(struct palloc *p, uint64_t count_frames);
#endif
