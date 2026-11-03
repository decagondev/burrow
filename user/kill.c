/* kill.c - kill [-SIGNAL] pid... (SIGTERM by default). */
#include <signal.h>
#include <stdio.h>
#include <stdlib.h>

int main(int argc, char **argv) {
    int sig = SIGTERM, i = 1;
    if (argc > 1 && argv[1][0] == '-') sig = atoi(argv[i++] + 1);
    int rc = 0;
    for (; i < argc; i++)
        if (kill(atoi(argv[i]), sig) != 0) { perror(argv[i]); rc = 1; }
    return rc;
}
