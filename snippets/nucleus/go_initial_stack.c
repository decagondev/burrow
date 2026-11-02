/* =============================================================================
 * go_initial_stack.c - build the stack a Linux process sees at its entry point.
 *
 * The stock Go runtime entry (_rt0_amd64_linux) expects, at RSP:
 *
 *     [rsp+0]   argc
 *     [rsp+8]   argv[0] ... argv[argc-1], NULL
 *               envp[0] ... envp[n-1],   NULL
 *               auxv: (type, value) pairs ..., (AT_NULL, 0)
 *     ...       strings and the 16 AT_RANDOM bytes, higher up
 *
 * The nucleus does this for the Go kernel in Wave 6, and the Go kernel does
 * the very same thing for user processes in Wave 9 (execve). One concept,
 * learned once, used twice.
 * ============================================================================= */
#include <stdint.h>
#include <stddef.h>

#define AT_NULL   0
#define AT_PHDR   3
#define AT_PHENT  4
#define AT_PHNUM  5
#define AT_PAGESZ 6
#define AT_ENTRY  9
#define AT_RANDOM 25
#define AT_EXECFN 31

struct exec_info { uint64_t phdr, phent, phnum, entry; };

static uint64_t push_bytes(uint64_t sp, const void *src, size_t n) {
    sp -= n;
    const uint8_t *s = src;
    for (size_t i = 0; i < n; i++) ((uint8_t *)sp)[i] = s[i];
    return sp;
}
static uint64_t push_str(uint64_t sp, const char *s) {
    size_t n = 0;
    while (s[n]) n++;
    return push_bytes(sp, s, n + 1);
}

/* stack_top: highest usable address of an already-mapped stack.
 * random16: 16 bytes of entropy (RDRAND/RDTSC mix) - Go seeds its hash maps from it.
 * Returns the RSP to start the thread with. */
uint64_t build_initial_stack(uint64_t stack_top, const struct exec_info *ex,
                             const uint8_t random16[16]) {
    static const char *const argv0 = "kernel";
    static const char *const envs[] = {
        "GODEBUG=asyncpreemptoff=1",   /* no signals -> no async preemption  */
        "GOTRACEBACK=all",             /* full goroutine dumps on panic       */
    };
    enum { NENV = sizeof envs / sizeof envs[0] };

    uint64_t sp = stack_top;
    uint64_t rnd = sp = push_bytes(sp, random16, 16);
    uint64_t a0 = sp = push_str(sp, argv0);
    uint64_t e[NENV];
    for (int i = 0; i < NENV; i++) e[i] = sp = push_str(sp, envs[i]);

    uint64_t auxv[][2] = {
        { AT_PAGESZ, 4096 }, { AT_RANDOM, rnd }, { AT_PHDR, ex->phdr },
        { AT_PHENT, ex->phent }, { AT_PHNUM, ex->phnum }, { AT_ENTRY, ex->entry },
        { AT_EXECFN, a0 }, { AT_NULL, 0 },
        /* No AT_SYSINFO_EHDR: without a vDSO the runtime falls back to real
         * clock_gettime syscalls. Add a vDSO later as an optimisation slice. */
    };
    size_t nwords = 1 /*argc*/ + 2 /*argv+NULL*/ + NENV + 1 + sizeof auxv / 8;
    sp = (sp - nwords * 8) & ~0xFull;          /* 16-byte aligned with argc at sp */

    uint64_t *w = (uint64_t *)sp;
    *w++ = 1;                                  /* argc */
    *w++ = a0; *w++ = 0;                       /* argv */
    for (int i = 0; i < NENV; i++) *w++ = e[i];
    *w++ = 0;                                  /* envp terminator */
    for (size_t i = 0; i < sizeof auxv / sizeof auxv[0]; i++) { *w++ = auxv[i][0]; *w++ = auxv[i][1]; }
    return sp;
}
