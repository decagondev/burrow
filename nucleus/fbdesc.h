/* fbdesc.h - the framebuffer stage2 (VBE, W21-S02) or the UEFI loader
 * (GOP, W26-S04) recorded in boot_info, read and checked. Pure: no hardware, no globals; host-tested in
 * tests/unit/c/test_fbdesc.c. Never trusts a field: a descriptor that is
 * not internally consistent is reported, not used.
 * Docs: docs/02-architecture/BOOT_PROTOCOL.md, WAVE-21-framebuffer.md. */
#ifndef NUCLEUS_FBDESC_H
#define NUCLEUS_FBDESC_H
#include <stdint.h>
#include "burrow/boot_info.h"

#define FB_MAX_SIDE_PIXELS  16384u
#define FB_MAX_BYTES        (256ull << 20)   /* nu_mmio_map's limit */
#define FB_PAGE_BYTES       4096u

struct fb_field { uint8_t size, pos; };      /* bits, lowest bit */

struct fb_desc {
    uint64_t phys;
    uint32_t width, height, pitch_bytes, bpp;
    struct fb_field red, green, blue;
};

enum fb_status {
    FB_OK = 0,
    FB_NONE,          /* no loader set a mode (fb_source neither VBE nor GOP) */
    FB_BAD_BASE,      /* zero, not page aligned, or past 2^46 */
    FB_BAD_SIZE,      /* a side of 0 or over FB_MAX_SIDE_PIXELS, or over FB_MAX_BYTES in all */
    FB_BAD_BPP,       /* not 15, 16, 24 or 32 */
    FB_BAD_PITCH,     /* shorter than a row of pixels */
    FB_BAD_FIELDS,    /* a colour field empty, past the pixel, or overlapping another */
};

/* Fills *out from bi's framebuffer fields when they make sense. */
enum fb_status fb_desc_from_bootinfo(const struct boot_info *bi, struct fb_desc *out);
const char *fb_status_str(enum fb_status st);
const char *fb_source_str(uint8_t source);   /* "vbe", "gop", "none" */
#endif
