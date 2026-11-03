/* mousetest.c - the mouse as Linux sees it (W14-S02). Tells the input
 * devices apart the way toolkits do (EVIOCGBIT: event0 has keys and no
 * relative axes, event1 has REL_X/REL_Y and BTN_LEFT), then draws a cursor
 * on /dev/fb0 that follows event1 from the middle of the screen, printing
 * button presses with the cursor's position. /dev/input/mice is read too
 * (3-byte PS/2 packets). A right click ends it. Uses only Linux interfaces.
 * tests/smoke/expect/devices.screen checks the cursor's pixels.
 * Docs: docs/04-plan/waves/WAVE-14-desktop-groundwork.md. */
#include <fcntl.h>
#include <linux/fb.h>
#include <linux/input.h>
#include <poll.h>
#include <stdint.h>
#include <stdio.h>
#include <string.h>
#include <sys/ioctl.h>
#include <sys/mman.h>
#include <unistd.h>

#define CURSOR 9
#define BITS_LONG (8 * sizeof(long))
#define TEST_BIT(b, n) ((b[(n) / BITS_LONG] >> ((n) % BITS_LONG)) & 1)

static uint32_t *fb;
static unsigned width, height, stride;

static int has(int fd, int type, int code) {
    unsigned long bits[KEY_MAX / BITS_LONG + 1];
    memset(bits, 0, sizeof bits);
    if (ioctl(fd, EVIOCGBIT(type, sizeof bits), bits) < 0) return 0;
    return TEST_BIT(bits, code);
}

static void square(int x, int y, uint32_t colour) {
    for (int j = y - CURSOR / 2; j <= y + CURSOR / 2; j++)
        for (int i = x - CURSOR / 2; i <= x + CURSOR / 2; i++)
            if (i >= 0 && j >= 0 && (unsigned)i < width && (unsigned)j < height) fb[j * stride + i] = colour;
}

static int open_fb(void) {
    int fd = open("/dev/fb0", O_RDWR);
    struct fb_var_screeninfo v;
    struct fb_fix_screeninfo f;
    if (fd < 0 || ioctl(fd, FBIOGET_VSCREENINFO, &v) || ioctl(fd, FBIOGET_FSCREENINFO, &f)) return -1;
    width = v.xres, height = v.yres, stride = f.line_length / 4;
    fb = mmap(0, f.smem_len, PROT_READ | PROT_WRITE, MAP_SHARED, fd, 0);
    if (fb == MAP_FAILED) return -1;
    memset(fb, 0, f.smem_len);
    return fd;
}

int main(void) {
    int kbd = open("/dev/input/event0", O_RDONLY | O_NONBLOCK);
    int mouse = open("/dev/input/event1", O_RDONLY | O_NONBLOCK);
    int mice = open("/dev/input/mice", O_RDONLY | O_NONBLOCK);
    if (kbd < 0 || mouse < 0 || mice < 0) { perror("mousetest: open"); return 1; }
    int kbd_ok = has(kbd, 0, EV_KEY) && !has(kbd, 0, EV_REL) && has(kbd, EV_KEY, KEY_A);
    int mouse_ok = has(mouse, 0, EV_REL) && has(mouse, EV_REL, REL_X) && has(mouse, EV_REL, REL_Y) &&
                   has(mouse, EV_KEY, BTN_LEFT) && !has(mouse, EV_KEY, KEY_A);
    char name[64] = "?";
    ioctl(mouse, EVIOCGNAME(sizeof name), name);
    printf("mousetest: event0 %s, event1 %s (\"%s\")\n", kbd_ok ? "keyboard" : "NOT a keyboard",
           mouse_ok ? "mouse" : "NOT a mouse", name);
    if (open_fb() < 0) { perror("mousetest: /dev/fb0"); return 1; }
    int x = width / 2, y = height / 2, packets = 0;
    square(x, y, 0xFFFFFF);
    printf("mousetest: reading, right click ends\n");
    fflush(stdout);
    for (;;) {
        struct pollfd p[2] = { { .fd = mouse, .events = POLLIN }, { .fd = mice, .events = POLLIN } };
        if (poll(p, 2, -1) < 0) { perror("mousetest: poll"); return 1; }
        unsigned char pkt[3 * 16];
        ssize_t m = read(mice, pkt, sizeof pkt);
        if (m > 0) packets += m / 3;
        struct input_event ev[16];
        ssize_t n = read(mouse, ev, sizeof ev);
        for (ssize_t i = 0; i < n / (ssize_t)sizeof ev[0]; i++) {
            if (ev[i].type == EV_REL && ev[i].code == REL_X) { square(x, y, 0); x += ev[i].value; square(x, y, 0xFFFFFF); }
            if (ev[i].type == EV_REL && ev[i].code == REL_Y) { square(x, y, 0); y += ev[i].value; square(x, y, 0xFFFFFF); }
            if (ev[i].type != EV_KEY || ev[i].value != 1) continue;
            if (ev[i].code == BTN_LEFT) printf("mousetest: left down at %d,%d\n", x, y);
            if (ev[i].code == BTN_RIGHT) {
                printf("mousetest: %s packets on /dev/input/mice\n", packets > 0 ? "saw" : "NO");
                printf("mousetest: done at %d,%d\n", x, y);
                return 0;
            }
        }
        fflush(stdout);
    }
}
