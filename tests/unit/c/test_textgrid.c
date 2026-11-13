/* test_textgrid.c - console text into cells (common/textgrid.c, W26-S04):
 * glyphs left to right, a new line clears its row and the next, a full line
 * wraps, the bottom wraps to the top, tabs and control characters. */
#include "minitest.h"
#include "textgrid.h"

static uint32_t put(struct textgrid *g, const char *s, struct textgrid_op *last) {
    uint32_t total = 0;
    struct textgrid_op ops[TEXTGRID_OPS_MAX];
    for (; *s; s++) {
        uint32_t n = textgrid_put(g, (unsigned char)*s, ops);
        if (n) *last = ops[n - 1];
        total += n;
    }
    return total;
}

TEST(textgrid_lines) {
    struct textgrid g;
    struct textgrid_op ops[TEXTGRID_OPS_MAX], last;
    CHECK(!textgrid_init(&g, 0, 10) && !textgrid_init(&g, 10, 1));
    CHECK(textgrid_init(&g, 4, 3));
    CHECK(put(&g, "ab", &last) == 2 && last.kind == TEXTGRID_GLYPH && last.col == 1 && last.row == 0 && last.c == 'b');
    CHECK(textgrid_put(&g, '\n', ops) == 2);
    CHECK(ops[0].kind == TEXTGRID_CLEAR_ROW && ops[0].row == 1 && ops[1].kind == TEXTGRID_CLEAR_ROW && ops[1].row == 2);
    CHECK(g.col == 0 && g.row == 1);
    CHECK(put(&g, "wxyz", &last) == 4 && last.col == 3 && last.row == 1);
    CHECK(textgrid_put(&g, 'q', ops) == 3);              /* a full line wraps before the glyph */
    CHECK(ops[0].row == 2 && ops[1].row == 0 && ops[2].kind == TEXTGRID_GLYPH && ops[2].col == 0 && ops[2].row == 2);
    CHECK(textgrid_put(&g, '\n', ops) == 2);             /* the bottom wraps to the top */
    CHECK(g.row == 0 && ops[0].row == 0 && ops[1].row == 1);
}

TEST(textgrid_controls) {
    struct textgrid g;
    struct textgrid_op ops[TEXTGRID_OPS_MAX];
    CHECK(textgrid_init(&g, 20, 4));
    CHECK(textgrid_put(&g, 'a', ops) == 1);
    CHECK(textgrid_put(&g, '\t', ops) == 0 && g.col == 8);
    CHECK(textgrid_put(&g, '\t', ops) == 0 && g.col == 16);
    CHECK(textgrid_put(&g, '\t', ops) == 0 && g.col == 20);   /* past the last stop: the next glyph wraps */
    CHECK(textgrid_put(&g, 'b', ops) == 3 && ops[2].col == 0 && ops[2].row == 1);
    CHECK(textgrid_put(&g, '\r', ops) == 0 && g.col == 0 && g.row == 1);
    CHECK(textgrid_put(&g, 0x1B, ops) == 0 && textgrid_put(&g, 0x7F, ops) == 0 && g.col == 0);
    CHECK(textgrid_put(&g, 0xC3, ops) == 1 && ops[0].c == 0xC3);   /* drawn as the font's box */
}
