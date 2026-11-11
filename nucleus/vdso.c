/* vdso.c - see vdso.h. One writer at a time (time_init on the BSP before
 * the APs start, then nu_set_realtime under Go's one RTC call), so the
 * sequence count needs no lock; readers in every process retry around it. */
#include "vdso.h"
#include "burrow/vdso.h"
#include "frames.h"
#include "boot.h"
#include "kprintf.h"
#include "lib_string.h"
#include "panic.h"
#include "errno.h"

#define PAGE_BYTES 4096u

extern const uint8_t vdso_image[], vdso_image_end[];   /* vdso_image.asm */

static uint64_t g_data_pa;
static volatile struct vdso_data *g_data;
static uint64_t g_code_pa[VDSO_MAX_CODE_PAGES];
static uint64_t g_code_pages, g_code_bytes;

void vdso_init(void) {
    g_code_bytes = (uint64_t)(vdso_image_end - vdso_image);
    g_code_pages = (g_code_bytes + PAGE_BYTES - 1) / PAGE_BYTES;
    if (g_code_pages == 0 || g_code_pages > VDSO_MAX_CODE_PAGES)
        panic("vdso: image of %llu bytes", (unsigned long long)g_code_bytes);
    g_data_pa = frame_alloc_zeroed();
    if (!g_data_pa) panic("vdso: no frame for the data page");
    g_data = (volatile struct vdso_data *)phys_to_virt(g_data_pa);
    for (uint64_t i = 0; i < g_code_pages; i++) {
        g_code_pa[i] = frame_alloc_zeroed();
        if (!g_code_pa[i]) panic("vdso: no frame for the code");
        uint64_t off = i * PAGE_BYTES, n = g_code_bytes - off < PAGE_BYTES ? g_code_bytes - off : PAGE_BYTES;
        memcpy(phys_to_virt(g_code_pa[i]), vdso_image + off, n);
    }
    kprintf("[nucleus] vdso %llu bytes in %llu pages ok\n",
            (unsigned long long)g_code_bytes, (unsigned long long)g_code_pages);
}

void vdso_update(uint64_t tsc0, uint64_t ns_mult, uint64_t realtime_base_ns) {
    if (!g_data) return;
    uint32_t s = g_data->seq;
    __atomic_store_n(&g_data->seq, s + 1, __ATOMIC_RELEASE);   /* odd: readers wait */
    __atomic_thread_fence(__ATOMIC_SEQ_CST);
    g_data->tsc0 = tsc0;
    g_data->ns_mult = ns_mult;
    g_data->realtime_base_ns = realtime_base_ns;
    g_data->ready = 1;
    __atomic_thread_fence(__ATOMIC_SEQ_CST);
    __atomic_store_n(&g_data->seq, s + 2, __ATOMIC_RELEASE);
}

long vdso_info(uint64_t *buf) {
    if (!g_data) return -ENODEV;
    buf[0] = g_data_pa;
    buf[1] = g_code_bytes;
    buf[2] = g_code_pages;
    for (uint64_t i = 0; i < g_code_pages; i++) buf[3 + i] = g_code_pa[i];
    return 0;
}
