/* font5x8.h - the console font in C (W26-S04): printable ASCII as 5x8
 * pixel art, a copy of kernel/gfx/font.go's glyphs so the loader and the
 * nucleus draw the same letters as the Go console before it starts.
 * Pure data: never touches hardware. kernel/gfx's TestCFontMatchesGo
 * checks every glyph against the Go source.
 * Docs: docs/04-plan/waves/WAVE-26-uefi.md (S04). */
#ifndef COMMON_FONT5X8_H
#define COMMON_FONT5X8_H
#include <stdint.h>

#define FONT5X8_W     5
#define FONT5X8_H     8
#define FONT5X8_FIRST 0x20
#define FONT5X8_LAST  0x7E

/* The glyph for c: FONT5X8_H rows, bit (FONT5X8_W - 1 - x) = ink at x.
 * Characters outside printable ASCII draw as a box, as in Go. */
const uint8_t *font5x8_glyph(unsigned char c);
#endif
