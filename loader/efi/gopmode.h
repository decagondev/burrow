/* gopmode.h - the graphics output protocol's modes read (W26-S04): a mode's
 * pixel format as boot_info's framebuffer fields (RGB and BGR with 8
 * reserved bits, a bit mask, or blit-only, which has no framebuffer), and
 * whether a mode is the one the image's "video" payload asks for. Pure: no
 * firmware calls (gop.c makes them), host-tested in tests/unit/c/test_gopmode.c.
 * UEFI 2.10, 12.9. Docs: docs/02-architecture/BOOT_PROTOCOL.md. */
#ifndef BURROW_EFI_GOPMODE_H
#define BURROW_EFI_GOPMODE_H
#include <stdint.h>
#include "efi.h"
#include "burrow/boot_info.h"
#include "burrow/manifest.h"

struct gop_layout {
    uint32_t bpp, pitch_bytes;
    uint8_t red_size, red_pos, green_size, green_pos, blue_size, blue_pos;
};

/* gop_layout_of fills *out for a mode with a framebuffer; 0 for a blit-only
 * mode, an unknown format, or masks boot_info cannot say (not contiguous,
 * empty, wider than 8 bits, or pixels of 8 bits or fewer). */
int gop_layout_of(const struct efi_gop_mode_info *mi, struct gop_layout *out);

/* gop_mode_fits: mi has a framebuffer and want's width and height (GOP's
 * pixels are what they are: want's bpp and pitch are stage2's to honour). */
int gop_mode_fits(const struct efi_gop_mode_info *mi, const struct video_request *want);

/* gop_fill_boot_info writes the framebuffer fields with fb_source GOP. */
void gop_fill_boot_info(struct boot_info *bi, const struct efi_gop_mode_info *mi,
                        const struct gop_layout *l, uint64_t fb_phys);
#endif
