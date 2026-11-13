/* fbcon.h - console text drawn on a linear framebuffer (W26-S04), for the
 * loader and the nucleus before the Go console starts: on a real PC there
 * is rarely a serial port, and a photo of the screen is the report a tester
 * can send. Cells come from textgrid.h, letters from font5x8.h (the Go
 * console's), light grey on black, the font scaled up on big screens.
 *
 * fbcon_tee is a char_sink that passes every character on (to the serial
 * line) and draws it once a screen is attached; what came before is kept
 * (up to FBCON_EARLY_BYTES) and drawn at attach, so the first lines show too.
 *
 * Pure: writes only the pixels it was given (the caller maps them), never
 * touches I/O; host-tested in tests/unit/c/test_fbcon.c.
 * Docs: docs/04-plan/waves/WAVE-26-uefi.md (S04). */
#ifndef COMMON_FBCON_H
#define COMMON_FBCON_H
#include <stdint.h>
#include "char_sink.h"
#include "textgrid.h"

#define FBCON_EARLY_BYTES 3072u
#define FBCON_CELL_W      6u    /* the glyph and a blank column */
#define FBCON_CELL_H      10u   /* the glyph and two blank rows */
#define FBCON_INK         0xC8u /* each of red, green and blue, out of 255 */

/* A framebuffer: bpp 15, 16, 24 or 32; each colour field size bits at pos. */
struct fbcon_screen {
    uint8_t *pixels;
    uint32_t width, height, pitch_bytes, bpp;
    uint8_t red_size, red_pos, green_size, green_pos, blue_size, blue_pos;
};

struct fbcon {
    struct fbcon_screen s;
    struct textgrid grid;
    uint32_t scale;        /* pixels per font pixel */
    uint32_t ink;          /* the packed pixel value of the text */
};

/* fbcon_init clears the screen and starts at the top left; 0 when the
 * screen cannot hold a console (an odd bpp, or too small). */
int  fbcon_init(struct fbcon *con, const struct fbcon_screen *s);
void fbcon_putc(struct fbcon *con, unsigned char c);

struct fbcon_tee {
    struct char_sink next;          /* every character goes here first */
    struct fbcon *con;              /* 0: not drawing (yet, or any more) */
    char early[FBCON_EARLY_BYTES];  /* before the first attach */
    uint32_t early_bytes;
    int attached_once;
};

void fbcon_tee_init(struct fbcon_tee *t, struct char_sink next);
struct char_sink fbcon_tee_sink(struct fbcon_tee *t);
/* attach draws what was kept (first attach only) and every character after. */
void fbcon_tee_attach(struct fbcon_tee *t, struct fbcon *con);
void fbcon_tee_detach(struct fbcon_tee *t);
#endif
