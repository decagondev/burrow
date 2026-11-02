/* elf_load.h - copy an ELF64 image's PT_LOAD segments into a physical window.
 * Pure logic: the window says which physical range may be written and gives
 * a pointer to its first byte (identity in the loader, a buffer in tests).
 * Every segment is validated BEFORE any byte is written, so a rejected image
 * leaves the window untouched. */
#ifndef LOADER_ELF_LOAD_H
#define LOADER_ELF_LOAD_H
#include <stdint.h>
#include <stddef.h>
#include "elf64.h"

#define ELF_LOAD_MAX_SEGMENTS 8u

struct load_window {
    uint64_t phys_base, phys_limit;   /* writable range [phys_base, phys_limit) */
    uint8_t *ptr;                     /* where phys_base is reachable           */
};

struct elf_loaded {
    uint64_t entry;
    uint32_t count;                   /* valid entries in seg[]                 */
    struct elf64_segment seg[ELF_LOAD_MAX_SEGMENTS];
};

/* Checks each PT_LOAD: [paddr, paddr+memsz) inside the window, no physical
 * overlap with another segment, vaddr and paddr share a page offset, and the
 * entry point lies in an executable segment. Then copies filesz bytes and
 * zeroes up to memsz (.bss). */
enum elf_status elf64_load(const void *img, size_t len, const struct load_window *w,
                           struct elf_loaded *out);
#endif
