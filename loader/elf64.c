#include "elf64.h"
#include "lib_string.h"

/* Our own copies of the on-disk structs: no dependency on host <elf.h>. */
struct ehdr {
    unsigned char ident[16];
    uint16_t type, machine;
    uint32_t version;
    uint64_t entry, phoff, shoff;
    uint32_t flags;
    uint16_t ehsize, phentsize, phnum, shentsize, shnum, shstrndx;
} __attribute__((packed));

struct phdr {
    uint32_t type, flags;
    uint64_t offset, vaddr, paddr, filesz, memsz, align;
} __attribute__((packed));

#define PT_LOAD 1
#define ET_EXEC 2
#define EM_X86_64 62

enum elf_status elf64_inspect(const void *img, size_t len, struct elf64_info *out) {
    const struct ehdr *h = img;
    if (len < sizeof *h)                      return ELF_TOO_SMALL;
    if (memcmp(h->ident, "\x7f" "ELF", 4))    return ELF_BAD_MAGIC;
    if (h->ident[4] != 2)                     return ELF_NOT_64BIT;
    if (h->ident[5] != 1)                     return ELF_NOT_LITTLE_ENDIAN;
    if (h->machine != EM_X86_64)              return ELF_NOT_X86_64;
    if (h->type != ET_EXEC)                   return ELF_NOT_EXEC;
    if (h->phentsize != sizeof(struct phdr))  return ELF_BAD_PHDRS;
    /* overflow-safe bounds check: phoff + phnum*phentsize <= len */
    uint64_t table = (uint64_t)h->phnum * sizeof(struct phdr);
    if (h->phoff > len || table > len - h->phoff) return ELF_BAD_PHDRS;
    out->entry = h->entry;
    out->phnum = h->phnum;
    return ELF_OK;
}

enum elf_status elf64_segment_at(const void *img, size_t len, uint16_t idx, struct elf64_segment *seg) {
    struct elf64_info info;
    enum elf_status st = elf64_inspect(img, len, &info);
    if (st != ELF_OK) return st;
    if (idx >= info.phnum) return ELF_BAD_PHDRS;

    const struct ehdr *h = img;
    const struct phdr *p = (const struct phdr *)((const uint8_t *)img + h->phoff) + idx;
    memset(seg, 0, sizeof *seg);
    if (p->type != PT_LOAD) return ELF_OK;           /* memsz stays 0 => skip */
    if (p->filesz > p->memsz)                        return ELF_BAD_SEGMENT;
    if (p->offset > len || p->filesz > len - p->offset) return ELF_BAD_SEGMENT;
    seg->vaddr = p->vaddr;  seg->paddr = p->paddr;
    seg->offset = p->offset; seg->filesz = p->filesz; seg->memsz = p->memsz;
    seg->flags = p->flags;
    return ELF_OK;
}

const char *elf_status_str(enum elf_status s) {
    static const char *const names[] = {
        "ok", "too small", "bad magic", "not 64-bit", "not little-endian",
        "not x86-64", "not ET_EXEC", "bad program headers", "bad segment",
        "segment outside load window", "segments overlap", "too many segments",
        "entry not in an executable segment",
    };
    return (unsigned)s < sizeof names / sizeof names[0] ? names[s] : "unknown";
}
