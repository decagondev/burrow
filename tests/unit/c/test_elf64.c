/* Builds tiny ELF images in memory with the HOST's <elf.h>, then checks our
 * independent parser agrees. Two implementations agreeing = confidence. */
#include <elf.h>
#include "minitest.h"
#include "elf64.h"

static unsigned char img[8192];   /* segment data lives at 0x1000..0x1100: buffer must cover it (BUG-0002) */

static void make_elf(void) {
    memset(img, 0, sizeof img);
    Elf64_Ehdr *h = (Elf64_Ehdr *)img;
    memcpy(h->e_ident, ELFMAG, SELFMAG);
    h->e_ident[EI_CLASS] = ELFCLASS64;
    h->e_ident[EI_DATA] = ELFDATA2LSB;
    h->e_type = ET_EXEC; h->e_machine = EM_X86_64;
    h->e_entry = 0xFFFFFFFF80100000ull;
    h->e_phoff = sizeof *h; h->e_phentsize = sizeof(Elf64_Phdr); h->e_phnum = 2;
    Elf64_Phdr *p = (Elf64_Phdr *)(img + h->e_phoff);
    p[0].p_type = PT_LOAD; p[0].p_offset = 0x1000; p[0].p_vaddr = 0xFFFFFFFF80100000ull;
    p[0].p_paddr = 0x100000; p[0].p_filesz = 0x100; p[0].p_memsz = 0x2000; p[0].p_flags = PF_R | PF_X;
    p[1].p_type = PT_NOTE;
}

TEST(elf_valid) {
    make_elf();
    struct elf64_info info;
    CHECK(elf64_inspect(img, sizeof img, &info) == ELF_OK);
    CHECK_EQ_U64(info.entry, 0xFFFFFFFF80100000ull);
    CHECK_EQ_U64(info.phnum, 2);
}
TEST(elf_load_segment) {
    make_elf();
    struct elf64_segment s;
    CHECK(elf64_segment_at(img, sizeof img, 0, &s) == ELF_OK);
    CHECK_EQ_U64(s.paddr, 0x100000); CHECK_EQ_U64(s.memsz, 0x2000); CHECK_EQ_U64(s.filesz, 0x100);
}
TEST(elf_non_load_is_skipped) {
    make_elf();
    struct elf64_segment s;
    CHECK(elf64_segment_at(img, sizeof img, 1, &s) == ELF_OK);
    CHECK_EQ_U64(s.memsz, 0);
}
TEST(elf_rejects) {
    struct elf64_info info;
    make_elf(); img[0] = 0;                           CHECK(elf64_inspect(img, sizeof img, &info) == ELF_BAD_MAGIC);
    make_elf(); img[EI_CLASS] = ELFCLASS32;           CHECK(elf64_inspect(img, sizeof img, &info) == ELF_NOT_64BIT);
    make_elf(); ((Elf64_Ehdr *)img)->e_machine = 3;   CHECK(elf64_inspect(img, sizeof img, &info) == ELF_NOT_X86_64);
    make_elf(); ((Elf64_Ehdr *)img)->e_phnum = 60000; CHECK(elf64_inspect(img, sizeof img, &info) == ELF_BAD_PHDRS);
    make_elf();                                       CHECK(elf64_inspect(img, 10, &info) == ELF_TOO_SMALL);
}
TEST(elf_rejects_segment_past_end) {
    make_elf();
    Elf64_Phdr *p = (Elf64_Phdr *)(img + sizeof(Elf64_Ehdr));
    p[0].p_filesz = 0x10000;  /* claims more file bytes than the image has */
    struct elf64_segment s;
    CHECK(elf64_segment_at(img, sizeof img, 0, &s) == ELF_BAD_SEGMENT);
    make_elf(); p[0].p_filesz = 0x3000;  /* filesz > memsz */
    CHECK(elf64_segment_at(img, sizeof img, 0, &s) == ELF_BAD_SEGMENT);
}
