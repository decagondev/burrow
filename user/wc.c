/* wc.c - count lines, words and bytes of stdin or files (-l, -w, -c pick one). */
#include <ctype.h>
#include <fcntl.h>
#include <stdio.h>
#include <string.h>
#include <unistd.h>

static void count(int fd, long n[3]) {
    char buf[4096];
    ssize_t k;
    int inword = 0;
    while ((k = read(fd, buf, sizeof buf)) > 0)
        for (ssize_t i = 0; i < k; i++) {
            n[2]++;
            if (buf[i] == '\n') n[0]++;
            if (isspace((unsigned char)buf[i])) inword = 0;
            else if (!inword) { inword = 1; n[1]++; }
        }
}

int main(int argc, char **argv) {
    int only = -1, i = 1;
    if (argc > 1 && argv[1][0] == '-') {
        only = strchr("lwc", argv[1][1]) ? (int)(strchr("lwc", argv[1][1]) - "lwc") : -1;
        i = 2;
    }
    long n[3] = { 0, 0, 0 };
    if (i >= argc) count(0, n);
    for (; i < argc; i++) {
        int fd = open(argv[i], O_RDONLY);
        if (fd < 0) { perror(argv[i]); return 1; }
        count(fd, n);
        close(fd);
    }
    if (only >= 0) printf("%ld\n", n[only]);
    else printf("%ld %ld %ld\n", n[0], n[1], n[2]);
    return 0;
}
