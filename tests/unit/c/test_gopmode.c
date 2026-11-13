/* test_gopmode.c - GOP modes as boot_info framebuffers (loader/efi/gopmode.c,
 * W26-S04): each pixel format, the masks a descriptor cannot say, the mode
 * the video payload asks for, and the fields that come out passing the
 * nucleus's check (nucleus/fbdesc.c). */
#include <string.h>
#include "minitest.h"
#include "gopmode.h"
#include "fbdesc.h"

static struct efi_gop_mode_info mode(uint32_t w, uint32_t h, uint32_t fmt) {
    struct efi_gop_mode_info mi;
    memset(&mi, 0, sizeof mi);
    mi.horizontal_resolution = w, mi.vertical_resolution = h;
    mi.pixel_format = fmt, mi.pixels_per_scan_line = w;
    return mi;
}

TEST(gopmode_formats) {
    struct gop_layout l;
    struct efi_gop_mode_info mi = mode(1024, 768, EFI_PIXEL_BGRX_8);   /* OVMF on QEMU's VGA */
    CHECK(gop_layout_of(&mi, &l) && l.bpp == 32 && l.pitch_bytes == 4096);
    CHECK(l.red_pos == 16 && l.green_pos == 8 && l.blue_pos == 0 && l.red_size == 8);
    mi = mode(800, 600, EFI_PIXEL_RGBX_8);
    mi.pixels_per_scan_line = 832;                                     /* padded */
    CHECK(gop_layout_of(&mi, &l) && l.red_pos == 0 && l.blue_pos == 16 && l.pitch_bytes == 3328);
    mi = mode(640, 480, EFI_PIXEL_BIT_MASK);                           /* RGB565 */
    mi.mask = (struct efi_pixel_bitmask){ 0xF800, 0x07E0, 0x001F, 0 };
    CHECK(gop_layout_of(&mi, &l) && l.bpp == 16 && l.pitch_bytes == 1280);
    CHECK(l.red_size == 5 && l.red_pos == 11 && l.green_size == 6 && l.green_pos == 5 && l.blue_pos == 0);
    mi.mask = (struct efi_pixel_bitmask){ 0x7C00, 0x03E0, 0x001F, 0 }; /* 555 */
    CHECK(gop_layout_of(&mi, &l) && l.bpp == 15);
    mi.mask = (struct efi_pixel_bitmask){ 0x3FF00000, 0x000FFC00, 0x000003FF, 0xC0000000 };  /* 10-bit */
    CHECK(!gop_layout_of(&mi, &l));
    mi.mask = (struct efi_pixel_bitmask){ 0xF0F000, 0x0F00, 0xFF, 0 };  /* a hole in red */
    CHECK(!gop_layout_of(&mi, &l));
    mi.mask = (struct efi_pixel_bitmask){ 0xFF0000, 0xFF8000, 0xFF, 0 };  /* overlapping */
    CHECK(!gop_layout_of(&mi, &l));
    mi.mask = (struct efi_pixel_bitmask){ 0xE0, 0x1C, 0x03, 0 };       /* 8 bits */
    CHECK(!gop_layout_of(&mi, &l));
    mi = mode(1024, 768, EFI_PIXEL_BLT_ONLY);
    CHECK(!gop_layout_of(&mi, &l));
    mi = mode(1024, 768, 7);
    CHECK(!gop_layout_of(&mi, &l));
    mi = mode(1024, 768, EFI_PIXEL_BGRX_8), mi.pixels_per_scan_line = 1000;
    CHECK(!gop_layout_of(&mi, &l));
}

TEST(gopmode_pick_and_fill) {
    struct video_request want = { "VIDR", 1024, 768, 32, { 0, 0, 0 }, 0 };
    struct efi_gop_mode_info mi = mode(1024, 768, EFI_PIXEL_BGRX_8);
    CHECK(gop_mode_fits(&mi, &want));
    mi.vertical_resolution = 600;
    CHECK(!gop_mode_fits(&mi, &want));
    mi = mode(1024, 768, EFI_PIXEL_BLT_ONLY);
    CHECK(!gop_mode_fits(&mi, &want));
    struct boot_info bi;
    struct gop_layout l;
    struct fb_desc d;
    memset(&bi, 0, sizeof bi);
    mi = mode(1280, 1024, EFI_PIXEL_BGRX_8);
    CHECK(gop_layout_of(&mi, &l));
    gop_fill_boot_info(&bi, &mi, &l, 0x80000000u);
    CHECK(bi.fb_source == FB_SOURCE_GOP && bi.fb_width == 1280 && bi.fb_pitch == 5120 && bi.fb_bpp == 32);
    CHECK(fb_desc_from_bootinfo(&bi, &d) == FB_OK && d.red.pos == 16 && d.phys == 0x80000000u);
    mi = mode(640, 480, EFI_PIXEL_BIT_MASK);
    mi.mask = (struct efi_pixel_bitmask){ 0xF800, 0x07E0, 0x001F, 0 };
    CHECK(gop_layout_of(&mi, &l));
    gop_fill_boot_info(&bi, &mi, &l, 0xC0000000u);
    CHECK(fb_desc_from_bootinfo(&bi, &d) == FB_OK && d.bpp == 16 && d.green.size == 6);
}
