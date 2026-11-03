/* spin.c - computes until interrupted (W10-S05): ^C at the terminal must
 * reach it as SIGINT although it never makes a system call while spinning. */
#include <stdio.h>

int main(void) {
    printf("spin: spinning until ^C\n");
    fflush(stdout);
    for (volatile unsigned long i = 0;; i++) { }
}
