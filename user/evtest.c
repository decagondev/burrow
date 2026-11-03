/* evtest.c - reads the keyboard through /dev/input/event0 (W11-S02): a
 * non-blocking read first (nothing queued: EAGAIN), the device's name by
 * ioctl, then events through poll until q is released. Prints every key
 * event as "evtest: key <code> down|up|repeat". While it runs, keys are
 * its own (the console gets none). Uses only Linux interfaces, like the
 * Doom backend. Docs: docs/04-plan/waves/WAVE-11-doom.md. */
#include <errno.h>
#include <fcntl.h>
#include <linux/input.h>
#include <poll.h>
#include <stdio.h>
#include <sys/ioctl.h>
#include <unistd.h>

int main(void) {
    int fd = open("/dev/input/event0", O_RDONLY | O_NONBLOCK);
    if (fd < 0) { perror("evtest: open /dev/input/event0"); return 1; }
    struct input_event ev[16];
    if (read(fd, ev, sizeof ev) < 0 && errno == EAGAIN) printf("evtest: nothing queued (EAGAIN)\n");
    char name[64] = "?";
    ioctl(fd, EVIOCGNAME(sizeof name), name);
    printf("evtest: device \"%s\"; reading, q quits\n", name);
    fflush(stdout);
    static const char *what[] = { "up", "down", "repeat" };
    for (;;) {
        struct pollfd p = { .fd = fd, .events = POLLIN };
        if (poll(&p, 1, -1) < 0) { perror("evtest: poll"); return 1; }
        ssize_t n = read(fd, ev, sizeof ev);
        for (ssize_t i = 0; i < n / (ssize_t)sizeof ev[0]; i++) {
            if (ev[i].type != EV_KEY || ev[i].value > 2) continue;
            printf("evtest: key %u %s\n", ev[i].code, what[ev[i].value]);
            if (ev[i].code == KEY_Q && ev[i].value == 0) {
                printf("evtest: done\n");
                return 0;
            }
        }
        fflush(stdout);
    }
}
