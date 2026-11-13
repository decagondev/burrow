/* gopmode.c - see gopmode.h. Pure: reads the mode it is given, writes the
 * struct it is given; no firmware calls, no globals. */
#include "gopmode.h"

static uint32_t lowest_bit(uint32_t m) {
    uint32_t i = 0;
    while (!(m & 1)) m >>= 1, i++;
    return i;
}

/* A mask as a field: contiguous ones, 1 to 8 of them. */
static int mask_field(uint32_t mask, uint8_t *size, uint8_t *pos) {
    if (!mask) return 0;
    uint32_t p = lowest_bit(mask), run = mask >> p;
    if (run & (run + 1)) return 0;               /* a hole in the run */
    uint32_t n = 0;
    while (run) run >>= 1, n++;
    if (n > 8) return 0;
    *size = (uint8_t)n;
    *pos = (uint8_t)p;
    return 1;
}

static uint32_t bits_used(const struct efi_pixel_bitmask *m) {
    uint32_t all = m->red | m->green | m->blue | m->reserved, n = 0;
    while (all) all >>= 1, n++;
    return n;
}

static int from_mask(const struct efi_pixel_bitmask *m, struct gop_layout *out) {
    if ((m->red & m->green) || (m->red & m->blue) || (m->green & m->blue)) return 0;
    if (!mask_field(m->red, &out->red_size, &out->red_pos) ||
        !mask_field(m->green, &out->green_size, &out->green_pos) ||
        !mask_field(m->blue, &out->blue_size, &out->blue_pos))
        return 0;
    uint32_t n = bits_used(m);
    if (n <= 8) return 0;
    out->bpp = n > 24 ? 32 : n > 16 ? 24 : n > 15 ? 16 : 15;
    return 1;
}

int gop_layout_of(const struct efi_gop_mode_info *mi, struct gop_layout *out) {
    struct gop_layout l = { 32, 0, 8, 0, 8, 8, 8, 0 };
    switch (mi->pixel_format) {
    case EFI_PIXEL_RGBX_8: l.red_pos = 0, l.blue_pos = 16; break;
    case EFI_PIXEL_BGRX_8: l.red_pos = 16, l.blue_pos = 0; break;
    case EFI_PIXEL_BIT_MASK:
        if (!from_mask(&mi->mask, &l)) return 0;
        break;
    default: return 0;   /* blit-only, or a format this loader does not know */
    }
    if (mi->horizontal_resolution == 0 || mi->vertical_resolution == 0 ||
        mi->pixels_per_scan_line < mi->horizontal_resolution)
        return 0;
    l.pitch_bytes = mi->pixels_per_scan_line * ((l.bpp + 7) / 8);
    *out = l;
    return 1;
}

int gop_mode_fits(const struct efi_gop_mode_info *mi, const struct video_request *want) {
    struct gop_layout l;
    return gop_layout_of(mi, &l) && mi->horizontal_resolution == want->width &&
           mi->vertical_resolution == want->height;
}

void gop_fill_boot_info(struct boot_info *bi, const struct efi_gop_mode_info *mi,
                        const struct gop_layout *l, uint64_t fb_phys) {
    bi->fb_addr = fb_phys;
    bi->fb_width = mi->horizontal_resolution;
    bi->fb_height = mi->vertical_resolution;
    bi->fb_pitch = l->pitch_bytes;
    bi->fb_bpp = l->bpp;
    bi->fb_red_size = l->red_size, bi->fb_red_pos = l->red_pos;
    bi->fb_green_size = l->green_size, bi->fb_green_pos = l->green_pos;
    bi->fb_blue_size = l->blue_size, bi->fb_blue_pos = l->blue_pos;
    bi->fb_source = FB_SOURCE_GOP;
}
