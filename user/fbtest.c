/* fbtest.c - draws through /dev/fb0 (W11-S01): asks the fbdev ioctls for
 * the mode, maps the framebuffer with MAP_SHARED, paints eight colour bars
 * across the whole screen (rows from line_length, PIT-27), waits for Enter
 * and lets go. tests/smoke/expect/devices.screen checks the bars' pixels.
 * Uses only Linux interfaces, so it runs unchanged on a Linux fbdev
 * console. Docs: docs/04-plan/waves/WAVE-11-doom.md. */
#include <fcntl.h>
#include <linux/fb.h>
#include <stdint.h>
#include <stdio.h>
#include <sys/ioctl.h>
#include <sys/mman.h>
#include <unistd.h>

static const uint32_t bars[8] = {
    0xFFFFFF, 0xFFFF00, 0x00FFFF, 0x00FF00, 0xFF00FF, 0xFF0000, 0x0000FF, 0x000000,
};

int main(void) {
    int fd = open("/dev/fb0", O_RDWR);
    if (fd < 0) { perror("fbtest: open /dev/fb0"); return 1; }
    struct fb_var_screeninfo v;
    struct fb_fix_screeninfo f;
    if (ioctl(fd, FBIOGET_VSCREENINFO, &v) || ioctl(fd, FBIOGET_FSCREENINFO, &f)) {
        perror("fbtest: ioctl");
        return 1;
    }
    printf("fbtest: %ux%u, %u bpp, red at bit %u, line_length %u, smem_len %u\n",
           v.xres, v.yres, v.bits_per_pixel, v.red.offset, f.line_length, f.smem_len);
    if (v.bits_per_pixel != 32) { fprintf(stderr, "fbtest: need 32 bpp\n"); return 1; }
    uint8_t *fb = mmap(0, f.smem_len, PROT_READ | PROT_WRITE, MAP_SHARED, fd, 0);
    if (fb == MAP_FAILED) { perror("fbtest: mmap"); return 1; }
    for (uint32_t y = 0; y < v.yres; y++) {
        uint32_t *row = (uint32_t *)(fb + y * f.line_length);
        for (uint32_t x = 0; x < v.xres; x++) row[x] = bars[x * 8 / v.xres];
    }
    printf("fbtest: bars drawn, press Enter\n");
    fflush(stdout);
    char c;
    read(0, &c, 1);
    munmap(fb, f.smem_len);
    close(fd);
    printf("fbtest: done\n");
    return 0;
}
