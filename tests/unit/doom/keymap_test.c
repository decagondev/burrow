/* keymap_test.c - BUG-0004's regression test: the Doom backend's evdev ->
 * Doom key map. <linux/input.h> and doomkeys.h both define KEY_ENTER (28
 * vs 13); the backend must map evdev's Enter to Doom's and must not treat
 * evdev 13 (KEY_EQUAL) as Enter. Includes the backend itself (its main
 * renamed) so the real map_key is tested. Built and run by scripts/doom.sh
 * on the host, which has doomgeneric's headers. Never touches devices. */
#define main burrow_doom_main
#include "doomgeneric_burrow.c"
#undef main

/* The engine functions the backend calls, unused here. */
pixel_t *DG_ScreenBuffer;
void doomgeneric_Create(int argc, char **argv) { (void)argc; (void)argv; }
void doomgeneric_Tick(void) {}

static int checks, failures;

static void expect(unsigned evdev, unsigned char want, const char *what) {
    unsigned char got = map_key(evdev);
    checks++;
    if (got != want) {
        failures++;
        printf("FAIL: %s: evdev %u -> %u, want %u\n", what, evdev, got, want);
    }
}

int main(void) {
    expect(EVDEV_KEY_ENTER, KEY_ENTER, "Enter (evdev 28 -> Doom 13)");
    expect(13 /* evdev KEY_EQUAL */, 0, "evdev 13 is '=', not Enter");
    expect(KEY_UP, KEY_UPARROW, "up");
    expect(KEY_DOWN, KEY_DOWNARROW, "down");
    expect(KEY_LEFT, KEY_LEFTARROW, "left");
    expect(KEY_RIGHT, KEY_RIGHTARROW, "right");
    expect(KEY_LEFTCTRL, KEY_FIRE, "ctrl fires");
    expect(KEY_SPACE, KEY_USE, "space uses");
    expect(KEY_ESC, KEY_ESCAPE, "escape");
    expect(KEY_Y, 'y', "y confirms");
    expect(KEY_3, '3', "weapon 3");
    expect(KEY_F12, 0, "unmapped");
    printf("doom keymap: %d checks, %d failures\n", checks, failures);
    return failures != 0;
}
