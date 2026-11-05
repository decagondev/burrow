/* ktest_protect.c - the CPU's protections (W20-S02): with SMAP, ring 0
 * cannot read a user page outside stac/clac; with SMEP it cannot run one,
 * stac or not; with NX the nucleus's own data cannot run. Each test builds
 * a real user page (a process address space from uspace, so every level of
 * the page tables has the U bit) and loads its CR3 for a moment, with
 * interrupts off so nothing switches it back. */
#include "ktest.h"
#include "uspace.h"
#include "vmm.h"
#include "boot.h"
#include "trap.h"
#include "protect.h"
#include "cpu.h"

#define PROBE_VA 0x500000ull
#define RET_INSN 0xC3

static uint8_t g_nx_data[16] = {RET_INSN};   /* .data: NX under W^X */

int kt_smap_smep_user_page(void) {
    KT_CHECK(protect_smep() && protect_smap());
    KT_CHECK((read_cr4() & (CR4_SMEP | CR4_SMAP)) == (CR4_SMEP | CR4_SMAP));
    long h = uspace_create();
    long pa = uspace_page_alloc(1);
    KT_CHECK(h >= 1 && pa > 0);
    *(volatile uint8_t *)phys_to_virt((uint64_t)pa) = RET_INSN;
    KT_CHECK(uspace_map((uint64_t)h, PROBE_VA, (uint64_t)pa, 4096, UPROT_EXEC) == 0);
    uint64_t flags = irq_save(), kernel_cr3 = read_cr3();
    write_cr3(uspace_get((uint64_t)h)->pt.root);
    uint64_t v = 0;
    int read_plain = probe_read64(PROBE_VA, &v);
    uint64_t err_plain = trap_last.error;
    stac();
    int read_stac = probe_read64(PROBE_VA, &v);
    int exec_stac = probe_exec(PROBE_VA);
    clac();
    int exec_plain = probe_exec(PROBE_VA);
    write_cr3(kernel_cr3);
    irq_restore(flags);
    KT_CHECK(read_plain == -1 && (err_plain & 1) && !(err_plain & 4));   /* present, from ring 0: SMAP */
    KT_CHECK(read_stac == 0 && (v & 0xFF) == RET_INSN);                 /* stac lets it through */
    KT_CHECK(exec_stac == -1 && exec_plain == -1);                      /* SMEP: never */
    KT_CHECK(trap_last.vector == VEC_PF && (trap_last.error & 0x10));   /* an instruction fetch */
    KT_CHECK(uspace_destroy((uint64_t)h) == 0);
    KT_CHECK(uspace_page_free((uint64_t)pa, 1) == 0);
    return 0;
}

int kt_nx_kernel_data(void) {
    KT_CHECK(probe_exec((uint64_t)(uintptr_t)g_nx_data) == -1);
    KT_CHECK(trap_last.vector == VEC_PF && (trap_last.error & 0x11) == 0x11);   /* present page, fetch */
    return 0;
}
