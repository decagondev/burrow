/* sleep.c - sleep N seconds. */
#include <stdlib.h>
#include <unistd.h>

int main(int argc, char **argv) {
    if (argc > 1) sleep((unsigned)atoi(argv[1]));
    return 0;
}
