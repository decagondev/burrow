/* go_stack.h - the stack a Linux process sees at its entry point, built for
 * the Go kernel (W6) and later, by the Go kernel, for user programs (W9).
 * Pure logic: it writes through the stack's own addresses, which are
 * mapped in the current address space (or are a buffer in host tests).
 *
 *   [rsp]      argc
 *   [rsp+8]    argv..., NULL, envp..., NULL, auxv (type, value)..., AT_NULL
 *   higher     strings and the 16 AT_RANDOM bytes
 *
 * No AT_SYSINFO_EHDR: without a vDSO the runtime uses real clock_gettime. */
#ifndef NUCLEUS_GO_STACK_H
#define NUCLEUS_GO_STACK_H
#include <stdint.h>

#define AT_NULL   0
#define AT_PHDR   3
#define AT_PHENT  4
#define AT_PHNUM  5
#define AT_PAGESZ 6
#define AT_ENTRY  9
#define AT_RANDOM 25
#define AT_EXECFN 31

struct exec_info { uint64_t phdr, phent, phnum, entry; };

#define STACK_MAX_ARGS 16   /* argv and envp entries each; more are dropped */

/* stack_top: highest address + 1 of a mapped stack. Returns the entry RSP
 * (16-byte aligned, pointing at argc). argv (argv[0] = the program name)
 * and envp are NULL-terminated. */
uint64_t build_initial_stack(uint64_t stack_top, const struct exec_info *ex,
                             const uint8_t random16[16], const char *const *argv,
                             const char *const *envp);

/* Splits text in place at spaces, tabs and newlines into at most max words
 * (NUL-terminating each); returns how many. For the "kargs" boot module. */
unsigned split_words(char *text, const char **words, unsigned max);
#endif
