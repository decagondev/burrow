/* xaccel.c - turn the X server's pointer acceleration off (W22-S05), as
 * `xset m 1/1 0` does on the host. TinyX reads /dev/input/mice, whose moves
 * Burrow computes from QEMU's absolute tablet: accelerated, the pointer would
 * run ahead of the host's. startx runs it before the session. Waits up to
 * 10 s for the server; prints "xaccel: off" (or why not, exit 1).
 * Docs: docs/04-plan/waves/WAVE-22-sound-tablet.md. */
#include <X11/Xlib.h>
#include <stdio.h>
#include <time.h>

enum { TRIES = 100, TRY_NS = 100000000 }; /* a look every 0.1 s */

int main(void) {
    Display *d = 0;
    for (int i = 0; i < TRIES && !(d = XOpenDisplay(0)); i++) {
        struct timespec t = {0, TRY_NS};
        nanosleep(&t, 0);
    }
    if (!d) {
        fprintf(stderr, "xaccel: no X server\n");
        return 1;
    }
    XChangePointerControl(d, True, True, 1, 1, 0); /* 1/1: no acceleration */
    int num, den, threshold;
    XGetPointerControl(d, &num, &den, &threshold);
    XCloseDisplay(d);
    if (num != den) {
        fprintf(stderr, "xaccel: the server kept %d/%d\n", num, den);
        return 1;
    }
    printf("xaccel: off\n");
    return 0;
}
