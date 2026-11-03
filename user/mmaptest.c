/* mmaptest.c - mmap of files (W14-S05). For each file named on the command
 * line (and /tmp/mmap.dat, which it writes first: three pages and a bit)
 * it maps the whole file privately, compares the bytes with read(), checks
 * that the rest of the last page is zeros, maps it again from offset 4096,
 * writes to a writable private mapping and checks the file did not change,
 * forks (the child sees the write and changes its own copy only), and
 * unmaps. Prints "mmaptest: <path> ok" per file, then "mmaptest: ok"; a
 * failed check prints FAIL and exits 1. Never maps a file writable-shared:
 * Burrow refuses that (ENODEV), which it checks too.
 * Docs: docs/04-plan/waves/WAVE-14-desktop-groundwork.md. */
#include <errno.h>
#include <fcntl.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <sys/mman.h>
#include <sys/stat.h>
#include <sys/wait.h>
#include <unistd.h>

#define PAGE 4096L
#define MAX_BYTES (4L << 20)

static int fail(const char *path, const char *what) {
    printf("mmaptest: %s FAIL %s (errno %d)\n", path, what, errno);
    exit(1);
}

static void make_data(const char *path) {
    static char buf[3 * PAGE + 100];
    for (long i = 0; i < (long)sizeof buf; i++) buf[i] = (char)('a' + i / PAGE + i % 7);
    int fd = open(path, O_WRONLY | O_CREAT | O_TRUNC, 0644);
    if (fd < 0 || write(fd, buf, sizeof buf) != (long)sizeof buf) fail(path, "write");
    close(fd);
}

static char *slurp(const char *path, long *size) {
    struct stat st;
    if (stat(path, &st) != 0 || st.st_size <= 0 || st.st_size > MAX_BYTES) fail(path, "stat");
    char *b = malloc(st.st_size);
    int fd = open(path, O_RDONLY);
    long got = 0, n;
    while (got < st.st_size && (n = read(fd, b + got, st.st_size - got)) > 0) got += n;
    close(fd);
    if (got != st.st_size) fail(path, "read");
    *size = got;
    return b;
}

static void child_sees(const char *path, char *m) {
    pid_t pid = fork();
    if (pid == 0) {
        if (memcmp(m, "MAPPED", 6) != 0) _exit(2);
        memcpy(m, "CHILD!", 6);
        _exit(0);
    }
    int status;
    if (waitpid(pid, &status, 0) != pid || !WIFEXITED(status) || WEXITSTATUS(status) != 0) fail(path, "fork child");
    if (memcmp(m, "MAPPED", 6) != 0) fail(path, "the child's write reached the parent");
}

static void check(const char *path) {
    long size;
    char *want = slurp(path, &size);
    long len = (size + PAGE - 1) / PAGE * PAGE;
    int fd = open(path, O_RDONLY);
    char *m = mmap(0, size, PROT_READ, MAP_PRIVATE, fd, 0);
    if (m == MAP_FAILED) fail(path, "mmap");
    if (memcmp(m, want, size) != 0) fail(path, "bytes differ from read()");
    for (long i = size; i < len; i++)
        if (m[i]) fail(path, "the tail of the last page is not zero");
    if (size > PAGE) {
        char *o = mmap(0, size - PAGE, PROT_READ, MAP_SHARED, fd, PAGE);
        if (o == MAP_FAILED || memcmp(o, want + PAGE, size - PAGE) != 0) fail(path, "offset 4096");
        munmap(o, size - PAGE);
    }
    char *w = mmap(0, size, PROT_READ | PROT_WRITE, MAP_PRIVATE, fd, 0);
    if (w == MAP_FAILED) fail(path, "writable private mmap");
    memcpy(w, "MAPPED", 6);
    if (mmap(0, size, PROT_READ | PROT_WRITE, MAP_SHARED, fd, 0) != MAP_FAILED || errno != ENODEV)
        fail(path, "writable MAP_SHARED was not ENODEV");
    close(fd); /* the mappings outlive the descriptor */
    child_sees(path, w);
    free(want);
    want = slurp(path, &size);
    if (memcmp(want, m, 6) != 0 || memcmp(want, "MAPPED", 6) == 0) fail(path, "a private write reached the file");
    if (munmap(m, size) != 0 || munmap(w, size) != 0) fail(path, "munmap");
    free(want);
    printf("mmaptest: %s ok (%ld bytes)\n", path, size);
}

int main(int argc, char **argv) {
    setvbuf(stdout, 0, _IONBF, 0);
    make_data("/tmp/mmap.dat");
    check("/tmp/mmap.dat");
    for (int i = 1; i < argc; i++) check(argv[i]);
    printf("mmaptest: ok\n");
    return 0;
}
