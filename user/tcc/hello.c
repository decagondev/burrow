/* hello.c - compiled on Burrow by TinyCC (W12-S06): shipped as
 * /usr/share/burrow/hello.c so the ports session can compile and run it
 * with `tcc -static` and `tcc -run`. Uses stdio, malloc and a little
 * arithmetic, so a wrong libc or a broken link would show. */
#include <stdio.h>
#include <stdlib.h>
#include <string.h>

int main(int argc, char **argv) {
    int n = argc > 1 ? atoi(argv[1]) : 10;
    long *fib = malloc(sizeof *fib * (n + 1));
    fib[0] = 0;
    fib[1] = 1;
    for (int i = 2; i <= n; i++) fib[i] = fib[i - 1] + fib[i - 2];
    printf("hello from C compiled on Burrow: fib(%d) = %ld (%s)\n", n, fib[n], strlen(argv[0]) ? argv[0] : "?");
    free(fib);
    return 0;
}
