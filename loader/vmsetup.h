/* vmsetup.h - the nucleus's first address space (Wave 4). Pure logic on top
 * of paging.h. Three mappings, MEMORY_MAP.md §2:
 *   1. identity [0, 1 GiB), 2 MiB pages, RWX  - temporary: the loader keeps
 *      running from low memory until it jumps (PIT-21); plus the loader's
 *      own pages above 1 GiB, if any (the UEFI loader, W26-S03)
 *   2. physmap  PHYSMAP_BASE + [0, top of RAM below 4 GiB) and every usable
 *      range above 4 GiB (not the PCI hole), 2 MiB pages where aligned, RW, NX
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
    /* The running loader, identity-mapped too where it lies above the first
     * 1 GiB (UEFI firmware loads it anywhere, W26-S03); 0 bytes: nothing. */
    uint64_t loader_base, loader_bytes;
};

/* Builds all three mappings into c (pt_init already done). On failure *what
 * names the mapping that failed. */
enum pt_status vm_build(struct pt_ctx *c, const struct vm_plan *p, const char **what);
/* Bytes the physmap maps: RAM below 4 GiB up to its top (rounded up to
 * 2 MiB) plus the usable ranges above 4 GiB. */
uint64_t vm_physmap_size(const struct e820_entry *map, uint32_t count);
#endif
