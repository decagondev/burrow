/* test_fbcon.c - console text on a framebuffer (common/fbcon.c, W26-S04):
 * a glyph's pixels in XRGB and RGB565 with the colour packed by its fields,
 * the scale on a big screen, refusals, and the tee that keeps the lines
 * printed before a screen is attached. */
#include <string.h>
#include "minitest.h"
#include "fbcon.h"
#include "font5x8.h"

#define W 64u
#define H 40u
static uint8_t g_px[H][W * 4 + 8];   /* a padded pitch */

static struct fbcon_screen xrgb(void) {
    memset(g_px, 0x55, sizeof g_px);
    return (struct fbcon_screen){ &g_px[0][0], W, H, W * 4 + 8, 32, 8, 16, 8, 8, 8, 0 };
}

static uint32_t px32(uint32_t x, uint32_t y) {
    uint32_t v;
    memcpy(&v, &g_px[y][x * 4], 4);
    return v;
}

TEST(fbcon_draws_glyphs) {
    struct fbcon con;
    struct fbcon_screen s = xrgb();
    CHECK(fbcon_init(&con, &s));
    CHECK(con.scale == 1 && con.grid.cols == W / 6 && con.grid.rows == H / 10);
    CHECK(px32(0, 0) == 0 && px32(W - 1, H - 1) == 0 && g_px[0][W * 4] == 0x55);   /* cleared, padding untouched */
    fbcon_putc(&con, 'A');
    CHECK(con.ink == 0xC8C8C8u);
    CHECK(px32(0, 0) == 0 && px32(1, 0) == 0xC8C8C8u && px32(0, 1) == 0xC8C8C8u);   /* A: .###. / #...# */
    fbcon_putc(&con, '\n');
    fbcon_putc(&con, '|');
    CHECK(px32(2, 10) == 0xC8C8C8u && px32(2, 16) == 0xC8C8C8u && px32(1, 10) == 0);
}

TEST(fbcon_rgb565_and_scale) {
    static uint8_t big[60][1600 * 2];
    struct fbcon con;
    struct fbcon_screen s = { &big[0][0], 1600, 60, 3200, 16, 5, 11, 6, 5, 5, 0 };
    CHECK(fbcon_init(&con, &s));
    CHECK(con.scale == 3 && con.ink == ((0xC8u >> 3) << 11 | (0xC8u >> 2) << 5 | (0xC8u >> 3)));
    fbcon_putc(&con, '!');                 /* the stem at x 2, scaled: pixels 6..8 */
    uint16_t v;
    memcpy(&v, &big[0][7 * 2], 2);
    CHECK(v == con.ink);
    memcpy(&v, &big[0][5 * 2], 2);
    CHECK(v == 0);
    s.bpp = 8;
    CHECK(!fbcon_init(&con, &s));
    s.bpp = 16, s.pitch_bytes = 3198;      /* shorter than a row */
    CHECK(!fbcon_init(&con, &s));
    s.pitch_bytes = 3200, s.height = 59;   /* one row of cells at scale 3 */
    CHECK(!fbcon_init(&con, &s));
}

static char g_serial[64];
static unsigned g_serial_n;
static void serial_putc(void *ctx, char c) { (void)ctx; if (g_serial_n < sizeof g_serial) g_serial[g_serial_n++] = c; }

TEST(fbcon_tee_keeps_early_lines) {
    static struct fbcon_tee tee;
    struct fbcon con;
    struct fbcon_screen s = xrgb();
    g_serial_n = 0;
    fbcon_tee_init(&tee, (struct char_sink){ serial_putc, 0 });
    struct char_sink k = fbcon_tee_sink(&tee);
    k.putc(k.ctx, 'A');
    k.putc(k.ctx, '\n');
    CHECK(g_serial_n == 2 && tee.early_bytes == 2);
    CHECK(fbcon_init(&con, &s));
    fbcon_tee_attach(&tee, &con);          /* the early 'A' is drawn at the top left */
    CHECK(px32(1, 0) == con.ink && con.grid.row == 1);
    k.putc(k.ctx, '|');
    CHECK(px32(2, 10) == con.ink && g_serial_n == 3);
    fbcon_tee_detach(&tee);
    k.putc(k.ctx, '|');                    /* detached: serial only, nothing kept */
    CHECK(px32(8, 10) == 0 && g_serial_n == 4 && tee.early_bytes == 2);
    fbcon_tee_attach(&tee, &con);          /* a second attach draws no history */
    CHECK(con.grid.col == 1);
}
