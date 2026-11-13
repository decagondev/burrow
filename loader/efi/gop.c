/*
 * gop.c - the screen from the graphics output protocol (W26-S04): the mode
 * the image's "video" payload asks for when the firmware has it, else the
 * mode the firmware already set; its framebuffer into boot_info (fb_source
 * GOP) and handed to fbcon so the loader's lines show on the screen. A
 * blit-only mode has no framebuffer: boot_info says none and the Go kernel
 * falls back to DISPI, as on the BIOS path without a video payload.
 *
 * RULE: only while the boot services run; never writes the firmware's mode
 * table. The pure half (formats, the choice) is gopmode.c.
 * Docs: docs/02-architecture/BOOT_PROTOCOL.md, WAVE-26-uefi.md (S04).
 */
#include "efi_loader.h"
#include "gopmode.h"
#include "fbcon.h"
#include "kprintf.h"

/* The first mode that fits want, set; 0 if none fits or the firmware refuses. */
static int set_wanted_mode(struct efi_gop *gop, const struct video_request *want) {
    for (uint32_t m = 0; m < gop->mode->max_mode; m++) {
        uint64_t size = 0;
        struct efi_gop_mode_info *mi = 0;
        if (gop->query_mode(gop, m, &size, &mi) != EFI_SUCCESS || !mi || size < sizeof *mi) continue;
        if (!gop_mode_fits(mi, want)) continue;
        if (m == gop->mode->mode) return 1;
        efi_status st = gop->set_mode(gop, m);
        if (st == EFI_SUCCESS) return 1;
        kprintf("[efi] screen: gop mode %u refused (status %llx)\n", m, (unsigned long long)st);
    }
    kprintf("[efi] screen: no gop mode is %ux%u: the firmware's stays\n", want->width, want->height);
    return 0;
}

int efi_setup_screen(struct boot_info *bi, const struct video_request *want, struct fbcon_screen *out) {
    static const struct efi_guid gop_guid = EFI_GRAPHICS_OUTPUT_PROTOCOL_GUID;
    struct efi_gop *gop = 0;
    efi_status st = g_st->boot_services->locate_protocol(&gop_guid, 0, (void **)&gop);
    if (st != EFI_SUCCESS || !gop || !gop->mode || !gop->mode->info) {
        kprintf("[efi] screen: no graphics output protocol (dispi fallback)\n");
        return 0;
    }
    if (want) set_wanted_mode(gop, want);
    const struct efi_gop_mode_info *mi = gop->mode->info;
    struct gop_layout l;
    if (!gop_layout_of(mi, &l) || !gop->mode->frame_buffer_base) {
        kprintf("[efi] screen: gop mode %u has no framebuffer (format %u; dispi fallback)\n",
                gop->mode->mode, mi->pixel_format);
        return 0;
    }
    gop_fill_boot_info(bi, mi, &l, gop->mode->frame_buffer_base);
    kprintf("[efi] screen: gop mode %u, %ux%ux%u pitch %u at %llx\n", gop->mode->mode, bi->fb_width,
            bi->fb_height, bi->fb_bpp, bi->fb_pitch, (unsigned long long)bi->fb_addr);
    *out = (struct fbcon_screen){
        (uint8_t *)(uintptr_t)bi->fb_addr, bi->fb_width, bi->fb_height, bi->fb_pitch, bi->fb_bpp,
        bi->fb_red_size, bi->fb_red_pos, bi->fb_green_size, bi->fb_green_pos, bi->fb_blue_size, bi->fb_blue_pos,
    };
    return 1;
}
