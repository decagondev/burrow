/* fbbench.c - how fast a program draws on /dev/fb0 (W21-S04): full-screen
 * fills and one-text-row scrolls through the mapping, timed and printed.
 * The framebuffer is write-combining since W21-S04; QEMU's TCG ignores
 * memory types, so these numbers are a baseline for KVM or real hardware,
 * not a test of WC: make fb-bench prints them and checks nothing. Steps
 * rows by line_length, never by the width (PIT-27). Never changes the mode.
 * Docs: docs/04-plan/waves/WAVE-21-framebuffer.md. */
#include <fcntl.h>
#include <linux/fb.h>
#include <stdint.h>
#include <stdio.h>
#include <string.h>
#include <sys/ioctl.h>
#include <sys/mman.h>
#include <time.h>
#include <unistd.h>

#define ROUNDS       20
#define SCROLL_ROWS  16

static long now_us(void) {
    struct timespec ts;
    clock_gettime(CLOCK_MONOTONIC, &ts);
    return ts.tv_sec * 1000000 + ts.tv_nsec / 1000;
}

static void report(const char *what, long us, uint64_t bytes) {
    printf("fbbench: %s: %ld us each, %llu MB/s\n", what, us / ROUNDS,
           (unsigned long long)(us ? bytes * ROUNDS / (uint64_t)us : 0));
}

int main(void) {
    int fd = open("/dev/fb0", O_RDWR);
    struct fb_var_screeninfo v;
    struct fb_fix_screeninfo f;
    if (fd < 0 || ioctl(fd, FBIOGET_VSCREENINFO, &v) || ioctl(fd, FBIOGET_FSCREENINFO, &f)) {
        puts("fbbench: FAIL no /dev/fb0");
        return 1;
    }
    uint8_t *fb = mmap(0, f.smem_len, PROT_READ | PROT_WRITE, MAP_SHARED, fd, 0);
    if (fb == MAP_FAILED) { puts("fbbench: FAIL mmap"); return 1; }
    uint32_t row = v.xres * ((v.bits_per_pixel + 7) / 8), lines = v.yres;
    printf("fbbench: %ux%u, %u bpp, line_length %u\n", v.xres, v.yres, v.bits_per_pixel, f.line_length);
    long t0 = now_us();
    for (int r = 0; r < ROUNDS; r++)
        for (uint32_t y = 0; y < lines; y++) memset(fb + (uint64_t)y * f.line_length, r * 9, row);
    report("fill", now_us() - t0, (uint64_t)row * lines);
    t0 = now_us();
    for (int r = 0; r < ROUNDS; r++)
        for (uint32_t y = 0; y + SCROLL_ROWS < lines; y++)
            memmove(fb + (uint64_t)y * f.line_length, fb + (uint64_t)(y + SCROLL_ROWS) * f.line_length, row);
    report("scroll", now_us() - t0, (uint64_t)row * (lines - SCROLL_ROWS));
    munmap(fb, f.smem_len);
    close(fd);
    puts("fbbench: done");
    return 0;
}
