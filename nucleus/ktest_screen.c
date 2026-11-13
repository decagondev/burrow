/* ktest_screen.c - the nucleus's lines on a GOP framebuffer (W26-S04),
 * read back from the screen itself: a glyph printed through kprintf is in
 * the framebuffer at its cell, nothing is drawn while drawing is off (the
 * Go kernel's screen), and drawing comes back (a panic's). Without a GOP
 * screen (every BIOS boot) there is nothing to read and the test says so.
 * Never leaves drawing off. Docs: docs/04-plan/waves/WAVE-26-uefi.md (S04). */
#include "ktest.h"
#include "fbcon.h"
#include "kprintf.h"
#include "screen.h"

/* The pixel at (x, y) is the text's colour. */
static int inked(const struct fbcon *c, uint32_t x, uint32_t y) {
    uint32_t bytes = (c->s.bpp + 7) / 8, v = 0;
    const uint8_t *p = c->s.pixels + (uint64_t)y * c->s.pitch_bytes + (uint64_t)x * bytes;
    for (uint32_t i = 0; i < bytes; i++) v |= (uint32_t)p[i] << (8 * i);
    return v == c->ink;
}

/* A '|' at (col, row): its stem (glyph column 2) inked, column 0 not. */
static int bar_at(const struct fbcon *c, uint32_t col, uint32_t row) {
    uint32_t x = col * FBCON_CELL_W * c->scale, y = row * FBCON_CELL_H * c->scale;
    return inked(c, x + 2 * c->scale, y) && inked(c, x + 2 * c->scale, y + 6 * c->scale) && !inked(c, x, y);
}

int kt_screen_gop_text(void) {
    const struct fbcon *c = screen_console();
    if (!c) {
        kprintf("[ktest] screen: no gop framebuffer on this boot\n");
        return 0;
    }
    kprintf("[ktest] screen: ");
    uint32_t col = c->grid.col, row = c->grid.row;
    KT_CHECK(!bar_at(c, col, row) && !bar_at(c, col + 1, row));
    kprintf("|");
    KT_CHECK(bar_at(c, col, row));
    screen_draw(0);                    /* the Go kernel's screen: serial only */
    kprintf("|");
    int drawn_while_off = bar_at(c, col + 1, row);
    screen_draw(1);                    /* a panic takes it back */
    kprintf("|\n");
    KT_CHECK(!drawn_while_off);
    KT_CHECK(bar_at(c, col + 1, row));
    return 0;
}
