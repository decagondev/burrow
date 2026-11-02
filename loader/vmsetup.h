/* vmsetup.h - the nucleus's first address space (Wave 4). Pure logic on top
 * of paging.h. Three mappings, MEMORY_MAP.md §2:
 *   1. identity [0, 1 GiB), 2 MiB pages, RWX  - temporary: the loader keeps
 *      running from low memory until it jumps (PIT-21)
 *   2. physmap  PHYSMAP_BASE + [0, top of usable RAM), 2 MiB pages, RW, NX
 *   3. nucleus  each PT_LOAD at its vaddr, 4 KiB pages, W^X from ELF flags
 * NX bits are only used when the CPU supports them (EFER.NXE, PIT-20). */
#ifndef LOADER_VMSETUP_H
#define LOADER_VMSETUP_H
#include <stdint.h>
#include <burrow/boot_info.h>
#include "paging.h"
#include "elf_load.h"

struct vm_plan {
    const struct e820_entry *map;
    uint32_t count;
    const struct elf_loaded *nucleus;
    int use_nx;                       /* 0: never set PTE_NX */
};

/* Builds all three mappings into c (pt_init already done). On failure *what
 * names the mapping that failed. */
enum pt_status vm_build(struct pt_ctx *c, const struct vm_plan *p, const char **what);
/* Size of the physmap: top of usable RAM rounded up to 2 MiB. */
uint64_t vm_physmap_size(const struct e820_entry *map, uint32_t count);
#endif
