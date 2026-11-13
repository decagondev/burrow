/* textgrid.h - text into cells (W26-S04): where each character of a
 * stream of console text lands on a grid of cols x rows, and which rows to
 * clear. The loader and the nucleus print their lines on the firmware's
 * framebuffer with it before the Go console starts (fbcon.h draws the
 * cells). Pure: a cursor and arithmetic, no pixels, host-tested
 * (tests/unit/c/test_textgrid.c).
 *
 * The grid never scrolls (moving a whole framebuffer is slow on a real
 * card): a new line clears its row and the one after it, and the bottom
 * wraps to the top, so the newest line is always the one above a blank row.
 * Docs: docs/04-plan/waves/WAVE-26-uefi.md (S04). */
#ifndef COMMON_TEXTGRID_H
#define COMMON_TEXTGRID_H
#include <stdint.h>

#define TEXTGRID_TAB_COLS 8u
#define TEXTGRID_OPS_MAX  3u   /* a wrap's two cleared rows, then the glyph */

struct textgrid {
    uint32_t cols, rows;   /* at least 1 x 2 */
    uint32_t col, row;     /* the cursor: where the next glyph goes */
};

enum textgrid_kind { TEXTGRID_GLYPH = 1, TEXTGRID_CLEAR_ROW };

struct textgrid_op {
    enum textgrid_kind kind;
    uint32_t col, row;     /* GLYPH: the cell; CLEAR_ROW: the row (col 0) */
    unsigned char c;       /* GLYPH: the character */
};

/* textgrid_init starts at the top left of a cols x rows grid; 0 when it
 * is too small to be a console (fewer than 1 column or 2 rows). */
int textgrid_init(struct textgrid *g, uint32_t cols, uint32_t rows);

/* textgrid_put moves the cursor past c and fills out[] with what to draw,
 * in order; returns how many ops (at most TEXTGRID_OPS_MAX). '\n' starts a
 * new line, '\r' returns to column 0, '\t' moves to the next tab stop, other
 * control characters are dropped, everything else is a glyph (a full line
 * wraps first). */
uint32_t textgrid_put(struct textgrid *g, unsigned char c, struct textgrid_op out[TEXTGRID_OPS_MAX]);
#endif
