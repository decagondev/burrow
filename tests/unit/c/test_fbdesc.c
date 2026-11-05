/* test_fbdesc.c - the boot_info framebuffer check (nucleus/fbdesc.c,
 * W21-S02): QEMU's XRGB mode with a padded pitch, layouts QEMU never
 * produces (BGR, RGB565, 24 bpp), and each way a descriptor can be wrong. */
#include <string.h>
#include "minitest.h"
#include "fbdesc.h"

static struct boot_info xrgb(uint32_t w, uint32_t h, uint32_t pitch) {
    struct boot_info bi;
    memset(&bi, 0, sizeof bi);
    bi.fb_source = FB_SOURCE_VBE;
    bi.fb_addr = 0xFD000000u;
    bi.fb_width = w, bi.fb_height = h, bi.fb_pitch = pitch, bi.fb_bpp = 32;
    bi.fb_red_size = 8, bi.fb_red_pos = 16;
    bi.fb_green_size = 8, bi.fb_green_pos = 8;
    bi.fb_blue_size = 8, bi.fb_blue_pos = 0;
    return bi;
}

TEST(fbdesc_vbe_modes) {
    struct fb_desc d;
    struct boot_info bi = xrgb(1280, 1024, 5248);              /* padded: 5120 would be tight */
    CHECK(fb_desc_from_bootinfo(&bi, &d) == FB_OK);
    CHECK(d.width == 1280 && d.pitch_bytes == 5248 && d.red.pos == 16 && d.blue.size == 8);
    bi.fb_red_pos = 0, bi.fb_blue_pos = 16;                    /* BGR */
    CHECK(fb_desc_from_bootinfo(&bi, &d) == FB_OK && d.red.pos == 0);
    bi = xrgb(800, 600, 1600);                                 /* RGB565 */
    bi.fb_bpp = 16;
    bi.fb_red_size = 5, bi.fb_red_pos = 11, bi.fb_green_size = 6, bi.fb_green_pos = 5;
    bi.fb_blue_size = 5, bi.fb_blue_pos = 0;
    CHECK(fb_desc_from_bootinfo(&bi, &d) == FB_OK);
    bi = xrgb(1024, 768, 3072);                                /* 24 bpp, tight */
    bi.fb_bpp = 24;
    CHECK(fb_desc_from_bootinfo(&bi, &d) == FB_OK);
}

TEST(fbdesc_refusals) {
    struct fb_desc d;
    struct boot_info bi = xrgb(1024, 768, 4096);
    bi.fb_source = FB_SOURCE_NONE;
    CHECK(fb_desc_from_bootinfo(&bi, &d) == FB_NONE);
    bi = xrgb(1024, 768, 4096), bi.fb_addr = 0;
    CHECK(fb_desc_from_bootinfo(&bi, &d) == FB_BAD_BASE);
    bi = xrgb(1024, 768, 4096), bi.fb_addr = 0xFD000010u;
    CHECK(fb_desc_from_bootinfo(&bi, &d) == FB_BAD_BASE);
    bi = xrgb(0, 768, 4096);
    CHECK(fb_desc_from_bootinfo(&bi, &d) == FB_BAD_SIZE);
    bi = xrgb(16384, 16384, 65536);                            /* 1 GiB: past nu_mmio_map */
    CHECK(fb_desc_from_bootinfo(&bi, &d) == FB_BAD_SIZE);
    bi = xrgb(1024, 768, 4096), bi.fb_bpp = 8;
    CHECK(fb_desc_from_bootinfo(&bi, &d) == FB_BAD_BPP);
    bi = xrgb(1024, 768, 4092);                                /* one pixel short */
    CHECK(fb_desc_from_bootinfo(&bi, &d) == FB_BAD_PITCH);
    bi = xrgb(1024, 768, 4096), bi.fb_green_pos = 12;          /* overlaps red */
    CHECK(fb_desc_from_bootinfo(&bi, &d) == FB_BAD_FIELDS);
    bi = xrgb(1024, 768, 4096), bi.fb_red_pos = 28;            /* past 32 bits */
    CHECK(fb_desc_from_bootinfo(&bi, &d) == FB_BAD_FIELDS);
    bi = xrgb(1024, 768, 4096), bi.fb_blue_size = 0;
    CHECK(fb_desc_from_bootinfo(&bi, &d) == FB_BAD_FIELDS);
    CHECK(strcmp(fb_status_str(FB_BAD_PITCH), "pitch shorter than a row") == 0);
}
