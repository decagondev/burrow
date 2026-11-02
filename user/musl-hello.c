/* musl-hello.c - the first C program (W9-S06): static musl, unmodified.
 * Exercises what musl's start-up and stdio need from the kernel: TLS
 * (arch_prctl), stdout (writev, ioctl), the heap (brk or mmap), files
 * (open, read, close) and exit_group. */
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <unistd.h>

int main(int argc, char **argv) {
    printf("hello from musl in ring 3 (pid %d, %d args:", (int)getpid(), argc);
    for (int i = 0; i < argc; i++) printf(" %s", argv[i]);
    printf(")\n");

    char *copy = malloc(64 * 1024);       /* bigger than one page: brk or mmap */
    if (!copy) return 1;
    strcpy(copy, "malloc works");
    printf("%s\n", copy);
    free(copy);

    FILE *f = fopen("/etc/hostname", "r");
    char name[64] = "";
    if (!f || !fgets(name, sizeof name, f)) return 2;
    fclose(f);
    name[strcspn(name, "\n")] = 0;
    printf("hostname from the initrd: %s\n", name);
    return 0;
}
