/* fbdesc.c - see fbdesc.h. Pure: reads the struct it is given and nothing
 * else; never includes io.h or cpu.h (host-tested). */
#include "fbdesc.h"

#define PHYS_LIMIT (1ull << 46)

static uint32_t bytes_per_pixel(uint32_t bpp) { return (bpp + 7) / 8; }

static uint32_t field_mask(struct fb_field f) {
    return f.size >= 32 ? 0xFFFFFFFFu : ((1u << f.size) - 1) << f.pos;
}

/* Each field non-empty, inside the pixel's bits, and apart from the others. */
static int fields_ok(const struct fb_desc *d) {
    const struct fb_field f[3] = { d->red, d->green, d->blue };
    uint32_t seen = 0;
    for (int i = 0; i < 3; i++) {
        if (f[i].size == 0 || f[i].size > 8 || f[i].pos + f[i].size > d->bpp) return 0;
        uint32_t m = field_mask(f[i]);
        if (seen & m) return 0;
        seen |= m;
    }
    return 1;
}

enum fb_status fb_desc_from_bootinfo(const struct boot_info *bi, struct fb_desc *out) {
    if (bi->fb_source != FB_SOURCE_VBE && bi->fb_source != FB_SOURCE_GOP) return FB_NONE;
    struct fb_desc d = {
        .phys = bi->fb_addr, .width = bi->fb_width, .height = bi->fb_height,
        .pitch_bytes = bi->fb_pitch, .bpp = bi->fb_bpp,
        .red = { bi->fb_red_size, bi->fb_red_pos },
        .green = { bi->fb_green_size, bi->fb_green_pos },
        .blue = { bi->fb_blue_size, bi->fb_blue_pos },
    };
    if (d.phys == 0 || d.phys % FB_PAGE_BYTES || d.phys >= PHYS_LIMIT) return FB_BAD_BASE;
    if (d.width == 0 || d.height == 0 || d.width > FB_MAX_SIDE_PIXELS || d.height > FB_MAX_SIDE_PIXELS)
        return FB_BAD_SIZE;
    if (d.bpp != 15 && d.bpp != 16 && d.bpp != 24 && d.bpp != 32) return FB_BAD_BPP;
    if (d.pitch_bytes < d.width * bytes_per_pixel(d.bpp)) return FB_BAD_PITCH;
    if ((uint64_t)d.pitch_bytes * d.height > FB_MAX_BYTES) return FB_BAD_SIZE;
    if (!fields_ok(&d)) return FB_BAD_FIELDS;
    *out = d;
    return FB_OK;
}

const char *fb_source_str(uint8_t source) {
    return source == FB_SOURCE_VBE ? "vbe" : source == FB_SOURCE_GOP ? "gop" : "none";
}

const char *fb_status_str(enum fb_status st) {
    static const char *const names[] = {
        "ok", "none", "bad base address", "bad size", "bad bits per pixel",
        "pitch shorter than a row", "bad colour fields",
    };
    return (unsigned)st < sizeof names / sizeof names[0] ? names[st] : "unknown";
}
