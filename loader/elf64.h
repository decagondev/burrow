/* elf64.h - minimal, defensive ELF64 reader. Pure logic: give it bytes,
 * it tells you what to load where. It never touches memory it was not given.
 * Used in Wave 5 to load the nucleus and Wave 6 to load the Go kernel. */
#ifndef LOADER_ELF64_H
#define LOADER_ELF64_H
#include <stdint.h>
#include <stddef.h>

enum elf_status {
    ELF_OK = 0,
    ELF_TOO_SMALL,
    ELF_BAD_MAGIC,
    ELF_NOT_64BIT,
    ELF_NOT_LITTLE_ENDIAN,
    ELF_NOT_X86_64,
    ELF_NOT_EXEC,
    ELF_BAD_PHDRS,
    ELF_BAD_SEGMENT,
};

struct elf64_info {
    uint64_t entry;
    uint16_t phnum;
};

struct elf64_segment {   /* one PT_LOAD program header, validated */
    uint64_t vaddr, paddr, offset, filesz, memsz;
    uint32_t flags;      /* PF_X=1, PF_W=2, PF_R=4 */
};

enum elf_status elf64_inspect(const void *img, size_t len, struct elf64_info *out);
/* Returns ELF_OK and fills *seg if program header idx is PT_LOAD.
 * Returns ELF_OK with seg->memsz == 0 for non-LOAD headers (caller skips them). */
enum elf_status elf64_segment_at(const void *img, size_t len, uint16_t idx, struct elf64_segment *seg);
const char *elf_status_str(enum elf_status s);
#endif
