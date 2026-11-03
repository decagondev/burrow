/* cat.c - copy files (or stdin) to stdout. */
#include <fcntl.h>
#include <stdio.h>
#include <unistd.h>

static int copy(int fd) {
    char buf[4096];
    ssize_t n;
    while ((n = read(fd, buf, sizeof buf)) > 0)
        if (write(1, buf, (size_t)n) != n) return 1;
    return n < 0;
}

int main(int argc, char **argv) {
    if (argc < 2) return copy(0);
    int rc = 0;
    for (int i = 1; i < argc; i++) {
        int fd = open(argv[i], O_RDONLY);
        if (fd < 0) { perror(argv[i]); rc = 1; continue; }
        rc |= copy(fd);
        close(fd);
    }
    return rc;
}
