/* hello.c - a starter program in your Burrow home: compile it on Burrow
 * with `tcc -static -o hello hello.c`, run it with `./hello 30`. */
#include <stdio.h>
#include <stdlib.h>

int main(int argc, char **argv) {
    int n = argc > 1 ? atoi(argv[1]) : 10;
    long a = 0, b = 1;
    for (int i = 0; i < n; i++) {
        long t = a + b;
        a = b;
        b = t;
    }
    printf("fib(%d) = %ld, computed on Burrow\n", n, a);
    return 0;
}
