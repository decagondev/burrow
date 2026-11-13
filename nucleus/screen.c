/* screen.c - see screen.h. Only a GOP framebuffer is drawn on: on the BIOS
 * path stage2's VBE mode stays the Go kernel's alone, as before W26. The
 * framebuffer is mapped write-combining, as the Go kernel maps it later
 * (vm_map_mmio_type refuses a second cache type for a page, PIT-64). */
#include "screen.h"
#include "boot.h"
#include "fbcon.h"
#include "fbdesc.h"
#include "kprintf.h"
#include "pat.h"
#include "vmm.h"

static struct fbcon_tee g_tee;
static struct fbcon g_con;
static int g_have;   /* g_con draws on a mapped framebuffer */

struct char_sink screen_sink(struct char_sink serial) {
    fbcon_tee_init(&g_tee, serial);
    return fbcon_tee_sink(&g_tee);
}

void screen_init(void) {
    const struct boot_info *bi = boot_info();
    struct fb_desc d;
    if (bi->fb_source != FB_SOURCE_GOP || fb_desc_from_bootinfo(bi, &d) != FB_OK) return;
    void *va;
    if (vm_map_mmio_type(d.phys, (uint64_t)d.pitch_bytes * d.height, pat_wc_flags(), &va) != 0) {
        kprintf("[nucleus] screen: framebuffer already mapped another way: serial only\n");
        return;
    }
    struct fbcon_screen s = { va, d.width, d.height, d.pitch_bytes, d.bpp, d.red.size, d.red.pos,
                              d.green.size, d.green.pos, d.blue.size, d.blue.pos };
    if (!fbcon_init(&g_con, &s)) {
        kprintf("[nucleus] screen: %ux%ux%u cannot hold a console: serial only\n", d.width, d.height, d.bpp);
        return;
    }
    g_have = 1;
    fbcon_tee_attach(&g_tee, &g_con);
    kprintf("[nucleus] screen: text on the gop framebuffer (%ux%u cells)\n", g_con.grid.cols, g_con.grid.rows);
}

void screen_draw(int on) {
    if (!g_have) return;
    if (on) fbcon_tee_attach(&g_tee, &g_con);
    else fbcon_tee_detach(&g_tee);
}

void screen_release(void) {
    if (!g_have) return;
    kprintf("[nucleus] screen: handed to the Go kernel\n");
    screen_draw(0);
}

void screen_panic(void) { screen_draw(1); }

const struct fbcon *screen_console(void) { return g_have ? &g_con : 0; }
