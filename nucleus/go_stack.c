/* go_stack.c - see go_stack.h. Pure: host-tested in tests/unit/c/test_go_stack.c. */
#include "go_stack.h"
#include "lib_string.h"

static uint64_t push_bytes(uint64_t sp, const void *src, uint64_t n) {
    sp -= n;
    memcpy((void *)(uintptr_t)sp, src, n);
    return sp;
}

static uint64_t push_str(uint64_t sp, const char *s) { return push_bytes(sp, s, strlen(s) + 1); }

/* Pushes each string of a NULL-terminated list; returns how many (<= max). */
static unsigned push_list(uint64_t *sp, const char *const *list, uint64_t *addrs) {
    unsigned n = 0;
    for (; list[n] && n < STACK_MAX_ARGS; n++) addrs[n] = *sp = push_str(*sp, list[n]);
    return n;
}

uint64_t build_initial_stack(uint64_t stack_top, const struct exec_info *ex,
                             const uint8_t random16[16], const char *const *argv,
                             const char *const *envp) {
    uint64_t sp = stack_top;
    uint64_t rnd = sp = push_bytes(sp, random16, 16);
    uint64_t args[STACK_MAX_ARGS], env[STACK_MAX_ARGS];
    unsigned nargs = push_list(&sp, argv, args);
    unsigned nenv = push_list(&sp, envp, env);
    uint64_t a0 = nargs ? args[0] : 0;

    const uint64_t auxv[][2] = {
        { AT_PAGESZ, 4096 }, { AT_RANDOM, rnd }, { AT_PHDR, ex->phdr },
        { AT_PHENT, ex->phent }, { AT_PHNUM, ex->phnum }, { AT_ENTRY, ex->entry },
        { AT_EXECFN, a0 },
        { ex->sysinfo_ehdr ? AT_SYSINFO_EHDR : AT_IGNORE, ex->sysinfo_ehdr },   /* W25-S03 */
        { AT_NULL, 0 },
    };
    uint64_t nwords = 1 /* argc */ + nargs + 1 + nenv + 1 + sizeof auxv / 8;
    sp = (sp - nwords * 8) & ~0xFull;          /* 16-aligned with argc at sp (SysV) */

    uint64_t *w = (uint64_t *)(uintptr_t)sp;
    *w++ = nargs;
    for (unsigned i = 0; i < nargs; i++) *w++ = args[i];
    *w++ = 0;
    for (unsigned i = 0; i < nenv; i++) *w++ = env[i];
    *w++ = 0;
    for (unsigned i = 0; i < sizeof auxv / sizeof auxv[0]; i++) { *w++ = auxv[i][0]; *w++ = auxv[i][1]; }
    return sp;
}

static int is_space(char c) { return c == ' ' || c == '\t' || c == '\n' || c == '\r'; }

unsigned split_words(char *text, const char **words, unsigned max) {
    unsigned n = 0;
    while (*text && n < max) {
        while (is_space(*text)) *text++ = 0;
        if (!*text) break;
        words[n++] = text;
        while (*text && !is_space(*text)) text++;
        if (*text) *text++ = 0;                   /* terminate it even when it is the last one */
    }
    return n;
}
