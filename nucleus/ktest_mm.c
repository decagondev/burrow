/* ktest_mm.c - physical and virtual memory on the real machine. */
#include "ktest.h"
#include "frames.h"
#include "boot.h"

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
