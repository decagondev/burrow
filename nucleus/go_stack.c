#include "go_stack.h"
#include "lib_string.h"

#define MAX_ENV 16

static uint64_t push_bytes(uint64_t sp, const void *src, uint64_t n) {
    sp -= n;
    memcpy((void *)(uintptr_t)sp, src, n);
    return sp;
}

static uint64_t push_str(uint64_t sp, const char *s) { return push_bytes(sp, s, strlen(s) + 1); }

uint64_t build_initial_stack(uint64_t stack_top, const struct exec_info *ex,
                             const uint8_t random16[16], const char *argv0,
                             const char *const *envp) {
    uint64_t sp = stack_top;
    uint64_t rnd = sp = push_bytes(sp, random16, 16);
    uint64_t a0 = sp = push_str(sp, argv0);
    uint64_t env[MAX_ENV];
    unsigned nenv = 0;
    for (; envp[nenv] && nenv < MAX_ENV; nenv++) env[nenv] = sp = push_str(sp, envp[nenv]);

    const uint64_t auxv[][2] = {
        { AT_PAGESZ, 4096 }, { AT_RANDOM, rnd }, { AT_PHDR, ex->phdr },
        { AT_PHENT, ex->phent }, { AT_PHNUM, ex->phnum }, { AT_ENTRY, ex->entry },
        { AT_EXECFN, a0 }, { AT_NULL, 0 },
    };
    uint64_t nwords = 1 /* argc */ + 2 /* argv[0], NULL */ + nenv + 1 + sizeof auxv / 8;
    sp = (sp - nwords * 8) & ~0xFull;          /* 16-aligned with argc at sp (SysV) */

    uint64_t *w = (uint64_t *)(uintptr_t)sp;
    *w++ = 1;
    *w++ = a0;
    *w++ = 0;
    for (unsigned i = 0; i < nenv; i++) *w++ = env[i];
    *w++ = 0;
    for (unsigned i = 0; i < sizeof auxv / sizeof auxv[0]; i++) { *w++ = auxv[i][0]; *w++ = auxv[i][1]; }
    return sp;
}
