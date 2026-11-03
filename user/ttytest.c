/* ttytest.c - the terminal from a program's side (W10-S05): it is a TTY,
 * it has a size, and a read returns one edited line. */
#include <ctype.h>
#include <stdio.h>
#include <sys/ioctl.h>
#include <unistd.h>

int main(void) {
    struct winsize ws;
    if (!isatty(0) || ioctl(1, TIOCGWINSZ, &ws) != 0) return 1;
    printf("ttytest: stdin is a terminal of %dx%d; type a line\n", ws.ws_col, ws.ws_row);
    char line[128];
    if (!fgets(line, sizeof line, stdin)) return 2;
    for (char *c = line; *c; c++) *c = (char)toupper((unsigned char)*c);
    printf("ttytest: you typed: %s", line);
    return 0;
}
