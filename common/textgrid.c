/* textgrid.c - see textgrid.h. Pure: no hardware, no globals. */
#include "textgrid.h"

int textgrid_init(struct textgrid *g, uint32_t cols, uint32_t rows) {
    if (cols < 1 || rows < 2) return 0;
    g->cols = cols;
    g->rows = rows;
    g->col = g->row = 0;
    return 1;
}

/* new_line: the next row (the top after the bottom), cleared with the one after it. */
static uint32_t new_line(struct textgrid *g, struct textgrid_op *out) {
    g->col = 0;
    g->row = (g->row + 1) % g->rows;
    out[0] = (struct textgrid_op){ TEXTGRID_CLEAR_ROW, 0, g->row, 0 };
    out[1] = (struct textgrid_op){ TEXTGRID_CLEAR_ROW, 0, (g->row + 1) % g->rows, 0 };
    return 2;
}

uint32_t textgrid_put(struct textgrid *g, unsigned char c, struct textgrid_op out[TEXTGRID_OPS_MAX]) {
    if (c == '\n') return new_line(g, out);
    if (c == '\r') {
        g->col = 0;
        return 0;
    }
    if (c == '\t') {
        uint32_t next = (g->col / TEXTGRID_TAB_COLS + 1) * TEXTGRID_TAB_COLS;
        g->col = next < g->cols ? next : g->cols;   /* the next glyph wraps */
        return 0;
    }
    if (c < 0x20 || c == 0x7F) return 0;
    uint32_t n = 0;
    if (g->col >= g->cols) n = new_line(g, out);
    out[n++] = (struct textgrid_op){ TEXTGRID_GLYPH, g->col, g->row, c };
    g->col++;
    return n;
}
