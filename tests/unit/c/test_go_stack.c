/* The Linux entry stack, built into a host buffer and parsed back the way
 * the Go runtime's rt0 + sysargs read it. */
#include "minitest.h"
#include "go_stack.h"

static unsigned char stack[4096] __attribute__((aligned(16)));

TEST(go_stack_layout) {
    static const char *const envp[] = { "GODEBUG=asyncpreemptoff=1", "GOTRACEBACK=all", 0 };
    static const uint8_t rnd[16] = { 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16 };
    struct exec_info ex = { 0x7dfffffff040ull, 56, 6, 0x7e000007ed00ull, 0 };
    uint64_t top = (uint64_t)(uintptr_t)(stack + sizeof stack);
    static const char *const argv[] = { "kernel", "panic=boot", 0 };
    uint64_t sp = build_initial_stack(top, &ex, rnd, argv, envp);

    CHECK((sp & 0xF) == 0);
    CHECK(sp > (uint64_t)(uintptr_t)stack && sp < top);
    uint64_t *w = (uint64_t *)(uintptr_t)sp;
    CHECK_EQ_U64(w[0], 2);                                     /* argc */
    CHECK_STR((const char *)(uintptr_t)w[1], "kernel");
    CHECK_STR((const char *)(uintptr_t)w[2], "panic=boot");
    CHECK_EQ_U64(w[3], 0);
    CHECK_STR((const char *)(uintptr_t)w[4], "GODEBUG=asyncpreemptoff=1");
    CHECK_STR((const char *)(uintptr_t)w[5], "GOTRACEBACK=all");
    CHECK_EQ_U64(w[6], 0);
    uint64_t *aux = &w[7];
    int seen_random = 0, seen_entry = 0, seen_phdr = 0, seen_execfn = 0;
    for (; aux[0] != AT_NULL; aux += 2) {
        if (aux[0] == AT_RANDOM) {
            const uint8_t *r = (const uint8_t *)(uintptr_t)aux[1];
            seen_random = memcmp(r, rnd, 16) == 0 && aux[1] + 16 <= top;
        }
        if (aux[0] == AT_ENTRY) seen_entry = aux[1] == ex.entry;
        if (aux[0] == AT_PHDR) seen_phdr = aux[1] == ex.phdr;
        if (aux[0] == AT_EXECFN) seen_execfn = aux[1] == w[1];
        CHECK((uint64_t)(uintptr_t)aux < top);
    }
    CHECK(seen_random && seen_entry && seen_phdr && seen_execfn);
}

/* W25-S03: AT_SYSINFO_EHDR names the vDSO when there is one, and is
 * absent (AT_IGNORE in its place) when there is none. */
static int sysinfo_of(uint64_t vdso) {
    static const char *const argv[] = { "kernel", 0 }, *const envp[] = { 0 };
    static const uint8_t rnd[16] = { 0 };
    struct exec_info ex = { 0x7dfffffff040ull, 56, 6, 0x7e000007ed00ull, vdso };
    uint64_t top = (uint64_t)(uintptr_t)(stack + sizeof stack);
    uint64_t *w = (uint64_t *)(uintptr_t)build_initial_stack(top, &ex, rnd, argv, envp);
    uint64_t *aux = &w[1 + 1 + 1 + 1];   /* argc, argv[0], NULL, (no envp) NULL */
    for (; aux[0] != AT_NULL; aux += 2)
        if (aux[0] == AT_SYSINFO_EHDR) return aux[1] == vdso ? 1 : -1;
    return 0;
}

TEST(go_stack_sysinfo_ehdr) {
    CHECK(sysinfo_of(0x7d0000001000ull) == 1);
    CHECK(sysinfo_of(0) == 0);
}

TEST(go_stack_split_words) {
    char text[] = "  panic=boot\tquiet\n\nx \n";
    const char *words[2];
    CHECK_EQ_U64(split_words(text, words, 2), 2);
    CHECK_STR(words[0], "panic=boot");
    CHECK_STR(words[1], "quiet");                              /* "x" is past max */
    char blank[] = " \t\n";
    CHECK_EQ_U64(split_words(blank, words, 2), 0);
}
