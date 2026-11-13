/* screen.h - the nucleus's lines on the firmware's framebuffer (W26-S04).
 * When the UEFI loader set a GOP mode, the nucleus draws what it prints on
 * the screen as well as COM1 (common/fbcon.c, the Go console's font) from
 * its first line until the Go kernel starts, and again from a panic or a
 * fatal trap on: on a real PC without a serial port, a photo of the screen
 * is the report. Never draws while the Go kernel owns the screen otherwise.
 * Docs: docs/04-plan/waves/WAVE-26-uefi.md (S04), BOOT_PROTOCOL.md. */
#ifndef NUCLEUS_SCREEN_H
#define NUCLEUS_SCREEN_H
#include "char_sink.h"

/* screen_sink wraps the serial sink: every character goes on to it, and is
 * kept until screen_init can draw it. */
struct char_sink screen_sink(struct char_sink serial);
void screen_init(void);      /* after pat_init: maps a GOP framebuffer, draws the lines so far */
void screen_release(void);   /* the Go kernel takes the screen */
void screen_panic(void);     /* a panic or a fatal trap: draw again */

/* For ktest (kt_screen_gop_text): the console drawn on, 0 without a GOP
 * screen; screen_draw turns drawing off (0) and on (1) without a message. */
struct fbcon;
const struct fbcon *screen_console(void);
void screen_draw(int on);
#endif
