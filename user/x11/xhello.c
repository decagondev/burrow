/* xhello.c - the first X client (W16-S01, S03): connects to $DISPLAY,
 * opens a 640x400 window, draws three rectangles, a line in the core font
 * "fixed" and a line in DejaVu Sans through Xft, and reports what happens:
 * "xhello: connected ..." with the server's vendor and screen, "xhello:
 * exposed", each key ("xhello: key <keysym name>") and button ("xhello:
 * button N at X,Y"). q quits with "xhello: ok"; "--quit-after <ms>" quits
 * by itself (the census). Plain Xlib and Xft: the same binary runs against
 * Xvfb on Linux and Xfbdev on Burrow.
 * Docs: docs/04-plan/waves/WAVE-16-x11-desktop.md. */
#include <X11/Xft/Xft.h>
#include <X11/Xlib.h>
#include <X11/Xutil.h>
#include <poll.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <time.h>

static long ms_now(void) {
    struct timespec t;
    clock_gettime(CLOCK_MONOTONIC, &t);
    return t.tv_sec * 1000 + t.tv_nsec / 1000000;
}

static void draw(Display *d, Window w, GC gc, XftDraw *xd, XftFont *font, XftColor *ink) {
    int s = DefaultScreen(d);
    unsigned long colours[3] = {0xE03030, 0x30C050, 0x3060E0};
    XSetForeground(d, gc, 0x202430);
    XFillRectangle(d, w, gc, 0, 0, 640, 400);
    for (int i = 0; i < 3; i++) {
        XSetForeground(d, gc, colours[i]);
        XFillRectangle(d, w, gc, 40 + i * 200, 40, 160, 120);
    }
    XSetForeground(d, gc, WhitePixel(d, s));
    const char *core = "xhello: the core font \"fixed\"";
    XDrawString(d, w, gc, 40, 210, core, strlen(core));
    if (font) XftDrawStringUtf8(xd, ink, font, 40, 280, (const FcChar8 *)"Burrow + X11", 12);
}

int main(int argc, char **argv) {
    long quit_at = -1;
    if (argc == 3 && strcmp(argv[1], "--quit-after") == 0) quit_at = ms_now() + atol(argv[2]);
    setvbuf(stdout, 0, _IONBF, 0);
    Display *d = XOpenDisplay(0);
    if (!d) { fprintf(stderr, "xhello: cannot open display %s\n", getenv("DISPLAY")); return 1; }
    int s = DefaultScreen(d);
    printf("xhello: connected to %s, %s release %d, screen %dx%d depth %d\n", DisplayString(d),
           ServerVendor(d), VendorRelease(d), DisplayWidth(d, s), DisplayHeight(d, s), DefaultDepth(d, s));
    Window w = XCreateSimpleWindow(d, RootWindow(d, s), 20, 20, 640, 400, 0, 0, BlackPixel(d, s));
    XStoreName(d, w, "xhello");
    XSelectInput(d, w, ExposureMask | KeyPressMask | ButtonPressMask);
    GC gc = XCreateGC(d, w, 0, 0);
    XFontStruct *fixed = XLoadQueryFont(d, "fixed");
    if (fixed) XSetFont(d, gc, fixed->fid);
    XftFont *font = XftFontOpenName(d, s, "DejaVu Sans:pixelsize=48");
    XftDraw *xd = XftDrawCreate(d, w, DefaultVisual(d, s), DefaultColormap(d, s));
    XftColor ink;
    XftColorAllocName(d, DefaultVisual(d, s), DefaultColormap(d, s), "#ffffff", &ink);
    printf("xhello: core font %s, Xft font %s\n", fixed ? "fixed" : "MISSING", font ? "DejaVu Sans" : "MISSING");
    XMapWindow(d, w);
    for (;;) {
        while (XPending(d)) {
            XEvent e;
            XNextEvent(d, &e);
            if (e.type == Expose && e.xexpose.count == 0) {
                draw(d, w, gc, xd, font, &ink);
                printf("xhello: exposed\n");
            } else if (e.type == KeyPress) {
                KeySym k = XLookupKeysym(&e.xkey, 0);
                printf("xhello: key %s\n", XKeysymToString(k) ? XKeysymToString(k) : "?");
                if (k == XK_q) { printf("xhello: ok\n"); XCloseDisplay(d); return 0; }
            } else if (e.type == ButtonPress) {
                printf("xhello: button %d at %d,%d\n", e.xbutton.button, e.xbutton.x, e.xbutton.y);
            }
        }
        if (quit_at >= 0 && ms_now() >= quit_at) { printf("xhello: ok\n"); XCloseDisplay(d); return 0; }
        struct pollfd p = {ConnectionNumber(d), POLLIN, 0};
        poll(&p, 1, quit_at >= 0 ? 50 : -1);
    }
}
