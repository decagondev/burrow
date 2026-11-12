/* handoff.h - the back half both loaders share (W26-S03): the nucleus's ELF
 * into its physical window, its first page tables, boot_info v4 and the
 * jump. loader/main.c (BIOS: frames from palloc over the E820 map) and
 * loader/efi/main.c (UEFI: frames from one block the firmware gave) fill a
 * struct handoff and call these in order. Markers are "[<tag>] ...", so the
 * BIOS images keep their "[loader] ..." lines.
 *
 * RULE: no firmware and no disk here; everything arrives in memory. A
 * failure prints "[<tag>] PANIC: ..." and exits QEMU with failure.
 * Docs: docs/02-architecture/BOOT_PROTOCOL.md, docs/04-plan/waves/WAVE-26-uefi.md. */
#ifndef LOADER_HANDOFF_H
#define LOADER_HANDOFF_H
#include <stdint.h>
#include <burrow/boot_info.h>
#include "palloc.h"
#include "elf_load.h"

struct handoff {
    const char *tag;                       /* marker prefix: "loader" or "efi"          */
    struct palloc *frames;                 /* page tables come from here                */
    const struct e820_entry *map;          /* RAM for the physmap                       */
    uint32_t map_count;
    uint64_t loader_base, loader_bytes;    /* also identity-mapped (UEFI: anywhere)     */
    uint32_t module_count;
    struct boot_module modules[BOOT_MODULES_MAX];
};

void __attribute__((noreturn)) handoff_panic(const struct handoff *h, const char *what);
/* stage2, loader and nucleus are boot stages; every other payload is a module. */
int  handoff_is_boot_stage(const char *name);
void handoff_add_module(struct handoff *h, const char *name, uint64_t start, uint64_t size_bytes);
/* Copies the PT_LOAD segments into 1-4 MiB (layout.h). */
void handoff_load_nucleus(struct handoff *h, const uint8_t *img, uint64_t size_bytes, struct elf_loaded *out);
/* Identity + loader + physmap + nucleus (vmsetup.h); the PML4's physical address. */
uint64_t handoff_page_tables(struct handoff *h, const struct elf_loaded *n);
/* Call after the last frame is taken: adds "nucleus" and "loader-frames",
 * writes the modules and the v2+ fields, sets version 4 (rsdp_addr is the
 * caller's). */
void handoff_boot_info(struct handoff *h, struct boot_info *bi, const struct elf_loaded *n);
/* CR3, then the nucleus's entry with RDI = boot_info in the physmap. */
void __attribute__((noreturn)) handoff_enter(uint64_t pml4, const struct elf_loaded *n, const struct boot_info *bi);
#endif
