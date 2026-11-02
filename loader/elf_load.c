#include "elf_load.h"
#include "lib_string.h"

#define PF_X 1u

static enum elf_status check_segment(const struct elf64_segment *s, const struct load_window *w) {
    if (s->paddr < w->phys_base || s->paddr > w->phys_limit ||
        s->memsz > w->phys_limit - s->paddr)              return ELF_OUTSIDE_WINDOW;
    if (s->vaddr + s->memsz < s->vaddr)                   return ELF_BAD_SEGMENT;
    if ((s->vaddr ^ s->paddr) & 0xFFF)                    return ELF_BAD_SEGMENT;  /* cannot be paged */
    return ELF_OK;
}

static enum elf_status collect(const void *img, size_t len, const struct load_window *w,
                               struct elf_loaded *out) {
    struct elf64_info info;
    enum elf_status st = elf64_inspect(img, len, &info);
    if (st != ELF_OK) return st;
    out->entry = info.entry;
    out->count = 0;
    for (uint16_t i = 0; i < info.phnum; i++) {
        struct elf64_segment s;
        if ((st = elf64_segment_at(img, len, i, &s)) != ELF_OK) return st;
        if (s.memsz == 0) continue;                         /* not PT_LOAD, or empty */
        if (out->count == ELF_LOAD_MAX_SEGMENTS) return ELF_TOO_MANY_SEGMENTS;
        if ((st = check_segment(&s, w)) != ELF_OK) return st;
        for (uint32_t j = 0; j < out->count; j++) {
            const struct elf64_segment *o = &out->seg[j];
            if (s.paddr < o->paddr + o->memsz && o->paddr < s.paddr + s.memsz)
                return ELF_SEGMENTS_OVERLAP;
        }
        out->seg[out->count++] = s;
    }
    for (uint32_t j = 0; j < out->count; j++) {
        const struct elf64_segment *s = &out->seg[j];
        if ((s->flags & PF_X) && out->entry >= s->vaddr && out->entry - s->vaddr < s->memsz)
            return ELF_OK;
    }
    return ELF_BAD_ENTRY;
}

enum elf_status elf64_load(const void *img, size_t len, const struct load_window *w,
                           struct elf_loaded *out) {
    enum elf_status st = collect(img, len, w, out);
    if (st != ELF_OK) return st;
    for (uint32_t j = 0; j < out->count; j++) {
        const struct elf64_segment *s = &out->seg[j];
        uint8_t *dst = w->ptr + (s->paddr - w->phys_base);
        memcpy(dst, (const uint8_t *)img + s->offset, s->filesz);
        memset(dst + s->filesz, 0, s->memsz - s->filesz);
    }
    return ELF_OK;
}
