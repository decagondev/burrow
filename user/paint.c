/* paint.c - the W14 desktop groundwork in one program (M14). It takes the
 * screen the way an X server does (/dev/tty0: KDSETMODE KD_GRAPHICS,
 * KDSKBMODE K_OFF), maps /dev/fb0, and paints with the mouse
 * (/dev/input/event1): a palette of eight colours along the top, a white
 * canvas below; a left click on the palette picks a colour, dragging with
 * the left button held paints 5-pixel lines. The colours live in a forked
 * palette process: paint listens on /tmp/paint.sock, the palette process
 * connects, and each click on the palette asks it for the colour over that
 * connection (the roadmap's M14: a colour that arrives over a Unix socket). Keys come from
 * /dev/input/event0: c clears, Esc quits, and the console comes back
 * (KD_TEXT, K_XLATE). "paint crash" takes the screen and then dies of a
 * NULL read: the kernel must give the console back by itself. Uses only
 * Linux interfaces. Prints "paint: ready" and "paint: N strokes".
 * tests/smoke/expect/desk.screen checks its pixels.
 * Docs: docs/04-plan/waves/WAVE-14-desktop-groundwork.md. */
#include <fcntl.h>
#include <linux/fb.h>
#include <linux/input.h>
#include <linux/kd.h>
#include <poll.h>
#include <stdint.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <sys/ioctl.h>
#include <sys/mman.h>
#include <sys/socket.h>
#include <sys/un.h>
#include <sys/wait.h>
#include <unistd.h>

#define SWATCH 64
#define BRUSH 5
#define SOCK "/tmp/paint.sock"

static const uint32_t palette[8] = {0x000000, 0xFF0000, 0x00A000, 0x0000FF,
                                    0xFFC000, 0x00C0C0, 0xC000C0, 0x808080};
static uint32_t *fb;
static int width, height, stride;

static void fill(int x0, int y0, int x1, int y1, uint32_t c) {
    for (int y = y0 < 0 ? 0 : y0; y < y1 && y < height; y++)
        for (int x = x0 < 0 ? 0 : x0; x < x1 && x < width; x++) fb[y * stride + x] = c;
}

static void canvas(void) {
    fill(0, 0, width, height, 0xFFFFFF);
    for (int i = 0; i < 8; i++) fill(i * SWATCH, 0, (i + 1) * SWATCH, SWATCH, palette[i]);
}

/* line paints from (x0,y0) to (x1,y1) with the brush, below the palette. */
static void line(int x0, int y0, int x1, int y1, uint32_t c) {
    int dx = abs(x1 - x0), dy = abs(y1 - y0), n = dx > dy ? dx : dy;
    for (int i = 0; i <= n; i++) {
        int x = n ? x0 + (x1 - x0) * i / n : x0, y = n ? y0 + (y1 - y0) * i / n : y0;
        if (y - BRUSH / 2 >= SWATCH) fill(x - BRUSH / 2, y - BRUSH / 2, x + BRUSH / 2 + 1, y + BRUSH / 2 + 1, c);
    }
}

/* palette_service runs in the forked child: it connects to paint and
 * answers each swatch number with that swatch's colour until paint hangs up. */
static void palette_service(void) {
    struct sockaddr_un a = {.sun_family = AF_UNIX, .sun_path = SOCK};
    int s = socket(AF_UNIX, SOCK_STREAM, 0);
    if (connect(s, (struct sockaddr *)&a, sizeof a) != 0) _exit(1);
    unsigned char i;
    while (read(s, &i, 1) == 1) {
        uint32_t c = palette[i & 7];
        if (write(s, &c, sizeof c) != sizeof c) break;
    }
    _exit(0);
}

/* start_palette listens on SOCK, forks the palette process and returns
 * the connection to it. */
static int start_palette(pid_t *kid) {
    struct sockaddr_un a = {.sun_family = AF_UNIX, .sun_path = SOCK};
    unlink(SOCK);
    int l = socket(AF_UNIX, SOCK_STREAM, 0);
    if (l < 0 || bind(l, (struct sockaddr *)&a, sizeof a) != 0 || listen(l, 1) != 0) return -1;
    if ((*kid = fork()) == 0) palette_service();
    int c = accept(l, 0, 0);
    close(l);
    unlink(SOCK);
    return c;
}

/* ask gets swatch i's colour from the palette process. */
static uint32_t ask(int conn, int i) {
    unsigned char b = (unsigned char)i;
    uint32_t c = 0;
    if (write(conn, &b, 1) != 1 || read(conn, &c, sizeof c) != sizeof c) return 0;
    return c;
}

static int take_screen(int tty) {
    return ioctl(tty, KDSETMODE, KD_GRAPHICS) == 0 && ioctl(tty, KDSKBMODE, K_OFF) == 0;
}

static int open_fb(void) {
    int fd = open("/dev/fb0", O_RDWR);
    struct fb_var_screeninfo v;
    struct fb_fix_screeninfo f;
    if (fd < 0 || ioctl(fd, FBIOGET_VSCREENINFO, &v) || ioctl(fd, FBIOGET_FSCREENINFO, &f)) return -1;
    width = v.xres, height = v.yres, stride = f.line_length / 4;
    fb = mmap(0, f.smem_len, PROT_READ | PROT_WRITE, MAP_SHARED, fd, 0);
    return fb == MAP_FAILED ? -1 : fd;
}

int main(int argc, char **argv) {
    int tty = open("/dev/tty0", O_RDWR);
    if (tty < 0 || !take_screen(tty)) { perror("paint: /dev/tty0"); return 1; }
    if (argc > 1 && strcmp(argv[1], "crash") == 0) {
        printf("paint: crashing with the screen taken\n");
        fflush(stdout);
        return *(volatile int *)0;
    }
    pid_t kid;
    int conn = start_palette(&kid);
    if (conn < 0) { perror("paint: " SOCK); return 1; }
    int kbd = open("/dev/input/event0", O_RDONLY | O_NONBLOCK);
    int mouse = open("/dev/input/event1", O_RDONLY | O_NONBLOCK);
    if (kbd < 0 || mouse < 0 || open_fb() < 0) { perror("paint: open"); return 1; }
    canvas();
    int x = width / 2, y = height / 2, down = 0, strokes = 0;
    uint32_t colour = ask(conn, 0);
    printf("paint: ready, the palette answers on " SOCK "\n");
    fflush(stdout);
    for (int done = 0; !done;) {
        struct pollfd p[2] = {{.fd = mouse, .events = POLLIN}, {.fd = kbd, .events = POLLIN}};
        poll(p, 2, -1);
        struct input_event ev[32];
        ssize_t n = read(mouse, ev, sizeof ev);
        for (ssize_t i = 0; i < n / (ssize_t)sizeof ev[0]; i++) {
            int ox = x, oy = y;
            if (ev[i].type == EV_REL && ev[i].code == REL_X) x += ev[i].value;
            if (ev[i].type == EV_REL && ev[i].code == REL_Y) y += ev[i].value;
            x = x < 0 ? 0 : x >= width ? width - 1 : x;
            y = y < 0 ? 0 : y >= height ? height - 1 : y;
            if (down) line(ox, oy, x, y, colour);
            if (ev[i].type == EV_KEY && ev[i].code == BTN_LEFT) {
                down = ev[i].value && y >= SWATCH;
                if (ev[i].value && y < SWATCH) colour = ask(conn, x / SWATCH < 8 ? x / SWATCH : 7);
                if (down) strokes++;
            }
        }
        n = read(kbd, ev, sizeof ev);
        for (ssize_t i = 0; i < n / (ssize_t)sizeof ev[0]; i++) {
            if (ev[i].type != EV_KEY || ev[i].value != 1) continue;
            if (ev[i].code == KEY_ESC) done = 1;
            if (ev[i].code == KEY_C) canvas();
        }
    }
    close(conn); /* the palette process sees end of file and exits */
    waitpid(kid, 0, 0);
    ioctl(tty, KDSKBMODE, K_XLATE);
    ioctl(tty, KDSETMODE, KD_TEXT);
    printf("paint: %d strokes, last colour %06x\n", strokes, (unsigned)colour);
    return 0;
}
