/* =============================================================================
 * doomgeneric_burrow.c - Doom platform layer for Burrow (Epic E10, W11).
 *
 * KEY IDEA: this file only uses LINUX interfaces - fbdev (/dev/fb0) and
 * evdev (/dev/input/event0). Burrow implements small, compatible subsets of
 * both. So you can build and debug this backend on a plain Linux VM console
 * FIRST, then the same static binary (musl) runs on Burrow unchanged.
 *
 * Built by `make doom` (scripts/doom.sh) against doomgeneric's engine;
 * tests/unit/doom/keymap_test.c checks map_key (BUG-0004).
 *
 * Kernel features this needs (W9-W11):
 *   open/close/read (O_NONBLOCK), ioctl FBIOGET_VSCREENINFO/FSCREENINFO,
 *   mmap of /dev/fb0, clock_gettime(CLOCK_MONOTONIC), nanosleep, file I/O
 *   for the WAD (Freedoom: freely redistributable).
 * ============================================================================= */
#define _POSIX_C_SOURCE 200809L
#include <fcntl.h>
#include <linux/fb.h>
#include <linux/input.h>
#include <stdint.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <sys/ioctl.h>
#include <sys/mman.h>
#include <time.h>
#include <unistd.h>

/* PITFALL (BUG-0004): <linux/input.h> and doomkeys.h BOTH define KEY_ENTER
 * (evdev 28 vs Doom 13). Capture the evdev value, then let Doom's win. */
enum { EVDEV_KEY_ENTER = KEY_ENTER };
#undef KEY_ENTER

#include "doomgeneric.h"
#include "doomkeys.h"

static uint8_t *fb;              /* mapped framebuffer                       */
static struct fb_var_screeninfo vinfo;
static struct fb_fix_screeninfo finfo;
static int kbd_fd = -1;
static uint64_t start_ms;

/* small ring buffer of key events: Doom polls DG_GetKey until it returns 0 */
#define KEYQ 32
static struct { int pressed; unsigned char key; } keyq[KEYQ];
static unsigned kq_head, kq_tail;

static uint64_t now_ms(void) {
    struct timespec ts;
    clock_gettime(CLOCK_MONOTONIC, &ts);
    return (uint64_t)ts.tv_sec * 1000u + (uint64_t)ts.tv_nsec / 1000000u;
}

static void die(const char *what) { perror(what); exit(1); }

void DG_Init(void) {
    int fd = open("/dev/fb0", O_RDWR);
    if (fd < 0) die("open /dev/fb0");
    if (ioctl(fd, FBIOGET_VSCREENINFO, &vinfo) < 0) die("FBIOGET_VSCREENINFO");
    if (ioctl(fd, FBIOGET_FSCREENINFO, &finfo) < 0) die("FBIOGET_FSCREENINFO");
    if (vinfo.bits_per_pixel != 32) { fprintf(stderr, "need 32 bpp\n"); exit(1); }
    fb = mmap(NULL, finfo.smem_len, PROT_READ | PROT_WRITE, MAP_SHARED, fd, 0);
    if (fb == MAP_FAILED) die("mmap fb");
    kbd_fd = open("/dev/input/event0", O_RDONLY | O_NONBLOCK);
    if (kbd_fd < 0) die("open /dev/input/event0");
    start_ms = now_ms();
}

/* Integer-scale DG_ScreenBuffer (RESX x RESY, 0x00RRGGBB) and centre it. */
void DG_DrawFrame(void) {
    unsigned sx = vinfo.xres / DOOMGENERIC_RESX, sy = vinfo.yres / DOOMGENERIC_RESY;
    unsigned s = sx < sy ? sx : sy;
    if (s == 0) s = 1;
    unsigned ox = (vinfo.xres - DOOMGENERIC_RESX * s) / 2;
    unsigned oy = (vinfo.yres - DOOMGENERIC_RESY * s) / 2;
    for (unsigned y = 0; y < DOOMGENERIC_RESY * s && oy + y < vinfo.yres; y++) {
        uint32_t *row = (uint32_t *)(fb + (oy + y) * finfo.line_length) + ox;
        const uint32_t *src = DG_ScreenBuffer + (y / s) * DOOMGENERIC_RESX;
        for (unsigned x = 0; x < DOOMGENERIC_RESX * s && ox + x < vinfo.xres; x++)
            row[x] = src[x / s];
    }
}

void DG_SleepMs(uint32_t ms) {
    struct timespec ts = { ms / 1000, (long)(ms % 1000) * 1000000L };
    nanosleep(&ts, NULL);
}

uint32_t DG_GetTicksMs(void) { return (uint32_t)(now_ms() - start_ms); }

static unsigned char map_key(unsigned code) {
    switch (code) {
    case KEY_UP:        return KEY_UPARROW;
    case KEY_DOWN:      return KEY_DOWNARROW;
    case KEY_LEFT:      return KEY_LEFTARROW;
    case KEY_RIGHT:     return KEY_RIGHTARROW;
    case KEY_LEFTCTRL:  case KEY_RIGHTCTRL:  return KEY_FIRE;
    case KEY_SPACE:     return KEY_USE;
    case KEY_LEFTSHIFT: case KEY_RIGHTSHIFT: return KEY_RSHIFT;
    case KEY_ESC:       return KEY_ESCAPE;
    case EVDEV_KEY_ENTER: return KEY_ENTER;
    case KEY_Y:         return 'y';
    case KEY_N:         return 'n';
    case KEY_1 ... KEY_7: return (unsigned char)('1' + (code - KEY_1));
    default:            return 0;
    }
}

static void pump_input(void) {
    struct input_event ev;
    while (read(kbd_fd, &ev, sizeof ev) == (ssize_t)sizeof ev) {
        if (ev.type != EV_KEY || ev.value == 2) continue;      /* ignore autorepeat */
        unsigned char k = map_key(ev.code);
        if (!k || (kq_head + 1) % KEYQ == kq_tail) continue;   /* unmapped or full  */
        keyq[kq_head].pressed = ev.value == 1;
        keyq[kq_head].key = k;
        kq_head = (kq_head + 1) % KEYQ;
    }
}

int DG_GetKey(int *pressed, unsigned char *key) {
    pump_input();
    if (kq_tail == kq_head) return 0;
    *pressed = keyq[kq_tail].pressed;
    *key = keyq[kq_tail].key;
    kq_tail = (kq_tail + 1) % KEYQ;
    return 1;
}

void DG_SetWindowTitle(const char *title) { (void)title; }

int main(int argc, char **argv) {
    doomgeneric_Create(argc, argv);
    for (;;) doomgeneric_Tick();
}
