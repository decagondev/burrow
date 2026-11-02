#include "pmm.h"

static int overlaps(uint64_t a, uint64_t alen, uint64_t b, uint64_t blen) {
    return a < b + blen && b < a + alen;
}

void pmm_init(struct pmm *p, void *(*phys_to_ptr)(void *env, uint64_t pa), void *env) {
    p->phys_to_ptr = phys_to_ptr;
    p->env = env;
    p->free_head = 0;
    p->free_frames = 0;
    p->total_frames = 0;
}

static int frame_is_free_ram(uint64_t pa, const struct e820_entry *map, uint32_t count,
                             const struct pmm_range *reserved, uint32_t nreserved) {
    for (uint32_t i = 0; i < count; i++)        /* E820 entries may overlap: non-usable wins */
        if (map[i].type != E820_USABLE && overlaps(pa, PMM_FRAME, map[i].base, map[i].length)) return 0;
    for (uint32_t i = 0; i < nreserved; i++)
        if (overlaps(pa, PMM_FRAME, reserved[i].base, reserved[i].size_bytes)) return 0;
    return 1;
}

void pmm_add_e820(struct pmm *p, const struct e820_entry *map, uint32_t count,
                  const struct pmm_range *reserved, uint32_t nreserved) {
    for (uint32_t i = 0; i < count; i++) {
        if (map[i].type != E820_USABLE) continue;
        uint64_t start = (map[i].base + PMM_FRAME - 1) & ~(PMM_FRAME - 1);
        uint64_t end = (map[i].base + map[i].length) & ~(PMM_FRAME - 1);
        if (start == 0) start = PMM_FRAME;
        for (uint64_t pa = start; pa + PMM_FRAME <= end && pa >= start; pa += PMM_FRAME) {
            if (!frame_is_free_ram(pa, map, count, reserved, nreserved)) continue;
            pmm_free(p, pa);
            p->total_frames++;
        }
    }
}

uint64_t pmm_alloc(struct pmm *p) {
    uint64_t pa = p->free_head;
    if (!pa) return 0;
    p->free_head = *(uint64_t *)p->phys_to_ptr(p->env, pa);
    p->free_frames--;
    return pa;
}

void pmm_free(struct pmm *p, uint64_t pa) {
    *(uint64_t *)p->phys_to_ptr(p->env, pa) = p->free_head;
    p->free_head = pa;
    p->free_frames++;
}
