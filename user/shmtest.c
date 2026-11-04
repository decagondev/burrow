/* shmtest.c - shared memory between processes (W18-S03). Each check holds
 * on Linux too: `make ports-smoke` runs this binary on the host before it
 * boots Burrow. Covers MAP_SHARED|MAP_ANONYMOUS across fork (both ways),
 * memfd_create (ftruncate, the same bytes through write, read and a
 * mapping; MAP_PRIVATE of it is a private copy), POSIX shm_open between
 * two processes and shm_unlink, and MAP_PRIVATE anonymous memory staying
 * private after fork (copy-on-write, W18-S01). Prints "shmtest: <name> ok"
 * per check, then "shmtest: ok"; a failed check prints FAIL.
 * Docs: docs/04-plan/waves/WAVE-18-foundations.md. */
#define _GNU_SOURCE
#include <errno.h>
#include <fcntl.h>
#include <stdio.h>
#include <string.h>
#include <sys/mman.h>
#include <sys/stat.h>
#include <sys/wait.h>
#include <unistd.h>

enum { SIZE = 3 * 4096 };

static int failures;

static void check(const char *name, int good) {
    printf("shmtest: %s %s\n", name, good ? "ok" : "FAIL");
    if (!good) failures++;
}

/* child runs f in a child process and waits for it. */
static int child(void (*f)(char *), char *mem) {
    pid_t pid = fork();
    if (pid == 0) {
        f(mem);
        _exit(0);
    }
    int st;
    waitpid(pid, &st, 0);
    return WIFEXITED(st) && WEXITSTATUS(st) == 0;
}

static void child_writes(char *mem) { strcpy(mem + 4096, "from the child"); }

static void anon_shared(void) {
    char *m = mmap(NULL, SIZE, PROT_READ | PROT_WRITE, MAP_SHARED | MAP_ANONYMOUS, -1, 0);
    strcpy(m, "from the parent");
    int ok = child(child_writes, m);
    check("anon-shared-child-to-parent", ok && strcmp(m + 4096, "from the child") == 0);
    check("anon-shared-parent-to-child", strcmp(m, "from the parent") == 0);
    char *p = mmap(NULL, 4096, PROT_READ | PROT_WRITE, MAP_PRIVATE | MAP_ANONYMOUS, -1, 0);
    strcpy(p, "parent's");
    pid_t pid = fork();
    if (pid == 0) {
        strcpy(p, "child's!");
        _exit(strcmp(p, "child's!") != 0);
    }
    waitpid(pid, NULL, 0);
    check("anon-private-stays-private", strcmp(p, "parent's") == 0);
    munmap(m, SIZE);
    munmap(p, 4096);
}

static void memfd(void) {
    int fd = memfd_create("shmtest", MFD_CLOEXEC);
    check("memfd-create", fd >= 0);
    check("memfd-ftruncate", ftruncate(fd, SIZE) == 0);
    struct stat st;
    check("memfd-size", fstat(fd, &st) == 0 && st.st_size == SIZE);
    char *m = mmap(NULL, SIZE, PROT_READ | PROT_WRITE, MAP_SHARED, fd, 0);
    strcpy(m + 100, "through the mapping");
    char buf[32] = {0};
    check("memfd-map-then-read", pread(fd, buf, 19, 100) == 19 && strcmp(buf, "through the mapping") == 0);
    pwrite(fd, "through write", 14, 5000);
    check("memfd-write-then-map", strcmp(m + 5000, "through write") == 0);
    char *priv = mmap(NULL, SIZE, PROT_READ | PROT_WRITE, MAP_PRIVATE, fd, 0);
    strcpy(priv + 100, "private change");
    check("memfd-private-copy", strcmp(m + 100, "through the mapping") == 0 && strcmp(priv + 100, "private change") == 0);
    int ok = child(child_writes, m);
    check("memfd-shared-across-fork", ok && pread(fd, buf, 14, 4096) == 14 && memcmp(buf, "from the child", 14) == 0);
    munmap(m, SIZE);
    munmap(priv, SIZE);
    close(fd);
}

static void posix_shm(void) {
    shm_unlink("/burrow-shmtest");
    int fd = shm_open("/burrow-shmtest", O_RDWR | O_CREAT | O_EXCL, 0600);
    check("shm-open-create", fd >= 0 && ftruncate(fd, 4096) == 0);
    check("shm-open-excl", shm_open("/burrow-shmtest", O_RDWR | O_CREAT | O_EXCL, 0600) < 0 && errno == EEXIST);
    char *m = mmap(NULL, 4096, PROT_READ | PROT_WRITE, MAP_SHARED, fd, 0);
    close(fd); /* the mapping keeps it */
    pid_t pid = fork();
    if (pid == 0) { /* a second process opens it by name */
        int f2 = shm_open("/burrow-shmtest", O_RDWR, 0);
        char *m2 = mmap(NULL, 4096, PROT_READ | PROT_WRITE, MAP_SHARED, f2, 0);
        strcpy(m2 + 8, "opened by name");
        _exit(f2 < 0 || m2 == MAP_FAILED);
    }
    int st;
    waitpid(pid, &st, 0);
    check("shm-open-by-name", WIFEXITED(st) && WEXITSTATUS(st) == 0 && strcmp(m + 8, "opened by name") == 0);
    check("shm-unlink", shm_unlink("/burrow-shmtest") == 0 && shm_open("/burrow-shmtest", O_RDWR, 0) < 0 && errno == ENOENT);
    check("shm-mapping-outlives-name", strcmp(m + 8, "opened by name") == 0);
    munmap(m, 4096);
}

int main(void) {
    setvbuf(stdout, NULL, _IONBF, 0);
    anon_shared();
    memfd();
    posix_shm();
    printf("shmtest: %s\n", failures ? "FAIL" : "ok");
    return failures != 0;
}
