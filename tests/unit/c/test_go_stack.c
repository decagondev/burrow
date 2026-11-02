/* The Linux entry stack, built into a host buffer and parsed back the way
 * the Go runtime's rt0 + sysargs read it. */
#include "minitest.h"
#include "go_stack.h"

static unsigned char stack[4096] __attribute__((aligned(16)));

TEST(go_stack_layout) {
    static const char *const envp[] = { "GODEBUG=asyncpreemptoff=1", "GOTRACEBACK=all", 0 };
    static const uint8_t rnd[16] = { 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16 };
    struct exec_info ex = { 0x7dfffffff040ull, 56, 6, 0x7e000007ed00ull };
    uint64_t top = (uint64_t)(uintptr_t)(stack + sizeof stack);
    uint64_t sp = build_initial_stack(top, &ex, rnd, "kernel", envp);

    CHECK((sp & 0xF) == 0);
    CHECK(sp > (uint64_t)(uintptr_t)stack && sp < top);
    uint64_t *w = (uint64_t *)(uintptr_t)sp;
    CHECK_EQ_U64(w[0], 1);                                     /* argc */
    CHECK_STR((const char *)(uintptr_t)w[1], "kernel");
    CHECK_EQ_U64(w[2], 0);
    CHECK_STR((const char *)(uintptr_t)w[3], "GODEBUG=asyncpreemptoff=1");
    CHECK_STR((const char *)(uintptr_t)w[4], "GOTRACEBACK=all");
    CHECK_EQ_U64(w[5], 0);
    uint64_t *aux = &w[6];
    int seen_random = 0, seen_entry = 0, seen_phdr = 0;
    for (; aux[0] != AT_NULL; aux += 2) {
        if (aux[0] == AT_RANDOM) {
            const uint8_t *r = (const uint8_t *)(uintptr_t)aux[1];
            seen_random = memcmp(r, rnd, 16) == 0 && aux[1] + 16 <= top;
        }
        if (aux[0] == AT_ENTRY) seen_entry = aux[1] == ex.entry;
        if (aux[0] == AT_PHDR) seen_phdr = aux[1] == ex.phdr;
        CHECK((uint64_t)(uintptr_t)aux < top);
    }
    CHECK(seen_random && seen_entry && seen_phdr);
}
