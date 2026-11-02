/* go_load.h - start the Go kernel like Linux execve would (GO_RUNTIME_HOSTING.md §3):
 * map its PT_LOAD segments (supervisor, W^X), record its text range for the
 * syscall gate, map a 1 MiB stack with a guard page, build argc/argv/envp/
 * auxv, and create thread "go-m0" entering e_entry in ring 0 with IF on. */
#ifndef NUCLEUS_GO_LOAD_H
#define NUCLEUS_GO_LOAD_H

/* 0 if started, -1 if there is no "kernel" boot module. Panics on a bad ELF. */
int go_kernel_start(void);
#endif
