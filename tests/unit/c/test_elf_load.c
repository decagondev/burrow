/* elf64_load against a fake 16 KiB physical window at 0x100000. Images are
 * built with the host <elf.h>, like test_elf64.c. */
#include <elf.h>
#include "minitest.h"
#include "elf_load.h"

#define WIN_BASE 0x100000ull
static unsigned char img[0x3000];
static unsigned char window[0x4000];
static const struct load_window win = { WIN_BASE, WIN_BASE + sizeof window, window };

/* text: 0x100 file bytes at phys 0x100000 (R X); data: 0x10 file bytes, 0x1800 memory (R W). */
static Elf64_Phdr *make_image(void) {
    memset(img, 0, sizeof img);
    memset(window, 0xCC, sizeof window);
    Elf64_Ehdr *h = (Elf64_Ehdr *)img;
    memcpy(h->e_ident, ELFMAG, SELFMAG);
    h->e_ident[EI_CLASS] = ELFCLASS64; h->e_ident[EI_DATA] = ELFDATA2LSB;
    h->e_type = ET_EXEC; h->e_machine = EM_X86_64;
    h->e_entry = 0xFFFFFFFF80100010ull;
    h->e_phoff = sizeof *h; h->e_phentsize = sizeof(Elf64_Phdr); h->e_phnum = 2;
    Elf64_Phdr *p = (Elf64_Phdr *)(img + h->e_phoff);
    p[0] = (Elf64_Phdr){ PT_LOAD, PF_R | PF_X, 0x1000, 0xFFFFFFFF80100000ull, 0x100000, 0x100, 0x100, 0x1000 };
    p[1] = (Elf64_Phdr){ PT_LOAD, PF_R | PF_W, 0x2000, 0xFFFFFFFF80101000ull, 0x101000, 0x10, 0x1800, 0x1000 };
    memset(img + 0x1000, 0x90, 0x100);   /* "code" */
    memset(img + 0x2000, 0x42, 0x10);    /* "data" */
    return p;
}
static int window_untouched(void) {
    for (unsigned i = 0; i < sizeof window; i++) if (window[i] != 0xCC) return 0;
    return 1;
}

TEST(elf_load_copies_and_zeroes) {
    make_image();
    struct elf_loaded out;
    CHECK(elf64_load(img, sizeof img, &win, &out) == ELF_OK);
    CHECK_EQ_U64(out.entry, 0xFFFFFFFF80100010ull);
    CHECK_EQ_U64(out.count, 2);
    CHECK(window[0] == 0x90 && window[0xFF] == 0x90);
    CHECK(window[0x100] == 0xCC);                          /* past text memsz: untouched */
    CHECK(window[0x1000] == 0x42 && window[0x100F] == 0x42);
    CHECK(window[0x1010] == 0 && window[0x27FF] == 0);     /* .bss zeroed */
    CHECK(window[0x2800] == 0xCC);
}
TEST(elf_load_rejects_outside_window) {
    struct elf_loaded out;
    make_image()[1].p_paddr = 0x103000;                    /* 0x1800 bytes past the end */
    CHECK(elf64_load(img, sizeof img, &win, &out) == ELF_OUTSIDE_WINDOW);
    make_image()[0].p_paddr = 0xFF000;                     /* below the window */
    CHECK(elf64_load(img, sizeof img, &win, &out) == ELF_OUTSIDE_WINDOW);
    CHECK(window_untouched());
}
TEST(elf_load_rejects_overlap_and_bad_entry) {
    struct elf_loaded out;
    make_image()[1].p_paddr = 0x100000;
    ((Elf64_Phdr *)(img + sizeof(Elf64_Ehdr)))[1].p_vaddr = 0xFFFFFFFF80100000ull;
    CHECK(elf64_load(img, sizeof img, &win, &out) == ELF_SEGMENTS_OVERLAP);
    make_image();
    ((Elf64_Ehdr *)img)->e_entry = 0xFFFFFFFF80101000ull;  /* lands in the data segment */
    CHECK(elf64_load(img, sizeof img, &win, &out) == ELF_BAD_ENTRY);
    make_image()[0].p_vaddr += 0x10;                       /* page offsets differ */
    CHECK(elf64_load(img, sizeof img, &win, &out) == ELF_BAD_SEGMENT);
    CHECK(window_untouched());
    CHECK_STR(elf_status_str(ELF_SEGMENTS_OVERLAP), "segments overlap");
}
