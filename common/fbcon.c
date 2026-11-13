/* fbcon.c - see fbcon.h. Pure: writes the pixels in its fbcon_screen and
 * nothing else; no io.h, no cpu.h. */
#include "fbcon.h"
#include "font5x8.h"

static uint32_t bytes_per_pixel(uint32_t bpp) { return (bpp + 7) / 8; }

static uint32_t field(uint8_t size, uint8_t pos, uint32_t v255) {
    return size ? (v255 >> (8 - size)) << pos : 0;
}

static void put_pixel(const struct fbcon_screen *s, uint32_t x, uint32_t y, uint32_t v) {
    uint8_t *p = s->pixels + (uint64_t)y * s->pitch_bytes + (uint64_t)x * bytes_per_pixel(s->bpp);
    for (uint32_t i = 0; i < bytes_per_pixel(s->bpp); i++) p[i] = (uint8_t)(v >> (8 * i));
}

static void fill(const struct fbcon_screen *s, uint32_t y0, uint32_t rows) {
    for (uint32_t y = y0; y < y0 + rows && y < s->height; y++) {
        uint8_t *line = s->pixels + (uint64_t)y * s->pitch_bytes;
        for (uint32_t i = 0; i < s->width * bytes_per_pixel(s->bpp); i++) line[i] = 0;
    }
}

int fbcon_init(struct fbcon *con, const struct fbcon_screen *s) {
    if (!s->pixels || (s->bpp != 15 && s->bpp != 16 && s->bpp != 24 && s->bpp != 32)) return 0;
    if (s->red_size > 8 || s->green_size > 8 || s->blue_size > 8) return 0;
    if (s->pitch_bytes < s->width * bytes_per_pixel(s->bpp)) return 0;
    con->s = *s;
    con->scale = s->width >= 1600 ? 3 : s->width >= 800 ? 2 : 1;
    if (!textgrid_init(&con->grid, s->width / (FBCON_CELL_W * con->scale), s->height / (FBCON_CELL_H * con->scale)))
        return 0;
    con->ink = field(s->red_size, s->red_pos, FBCON_INK) | field(s->green_size, s->green_pos, FBCON_INK) |
               field(s->blue_size, s->blue_pos, FBCON_INK);
    fill(s, 0, s->height);
    return 1;
}

static void draw_glyph(const struct fbcon *con, uint32_t col, uint32_t row, unsigned char c) {
    const uint8_t *g = font5x8_glyph(c);
    uint32_t k = con->scale, x0 = col * FBCON_CELL_W * k, y0 = row * FBCON_CELL_H * k;
    for (uint32_t gy = 0; gy < FONT5X8_H * k; gy++)
        for (uint32_t gx = 0; gx < FONT5X8_W * k; gx++) {
            int ink = (g[gy / k] >> (FONT5X8_W - 1 - gx / k)) & 1;
            put_pixel(&con->s, x0 + gx, y0 + gy, ink ? con->ink : 0);
        }
}

void fbcon_putc(struct fbcon *con, unsigned char c) {
    struct textgrid_op ops[TEXTGRID_OPS_MAX];
    uint32_t n = textgrid_put(&con->grid, c, ops);
    for (uint32_t i = 0; i < n; i++) {
        if (ops[i].kind == TEXTGRID_CLEAR_ROW)
            fill(&con->s, ops[i].row * FBCON_CELL_H * con->scale, FBCON_CELL_H * con->scale);
        else
            draw_glyph(con, ops[i].col, ops[i].row, ops[i].c);
    }
}

static void tee_putc(void *ctx, char c) {
    struct fbcon_tee *t = ctx;
    t->next.putc(t->next.ctx, c);
    if (t->con) fbcon_putc(t->con, (unsigned char)c);
    else if (!t->attached_once && t->early_bytes < FBCON_EARLY_BYTES) t->early[t->early_bytes++] = c;
}

void fbcon_tee_init(struct fbcon_tee *t, struct char_sink next) {
    t->next = next;
    t->con = 0;
    t->early_bytes = 0;
    t->attached_once = 0;
}

struct char_sink fbcon_tee_sink(struct fbcon_tee *t) { return (struct char_sink){ tee_putc, t }; }

void fbcon_tee_attach(struct fbcon_tee *t, struct fbcon *con) {
    if (!t->attached_once)
        for (uint32_t i = 0; i < t->early_bytes; i++) fbcon_putc(con, (unsigned char)t->early[i]);
    t->attached_once = 1;
    t->con = con;
}

void fbcon_tee_detach(struct fbcon_tee *t) { t->con = 0; }
