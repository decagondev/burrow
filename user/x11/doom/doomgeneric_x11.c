/* =============================================================================
 * doomgeneric_x11.c - Doom's platform layer for an X window (W16-S05).
 *
 * The Xlib counterpart of user/doom/doomgeneric_burrow.c: the engine draws
 * 640x400 0x00RRGGBB pixels into DG_ScreenBuffer, and this file puts them in
 * a window with XPutImage and turns key events into Doom keys. It is ours,
 * not doomgeneric's doomgeneric_xlib.c, because that one needs XKB, which
 * TinyX does not have (WAVE-16 risks): keysyms come from XLookupKeysym, and
 * X's autorepeat (a release and a press with the same time) is filtered by
 * peeking at the queue instead of XkbSetDetectableAutoRepeat.
 *
 * When the server offers MIT-SHM (W18-S04: Burrow has SysV shared memory),
 * the frame goes into a segment both sides map and XShmPutImage only names
 * it; otherwise every frame crosses the socket with XPutImage.
 *
 * The window asks for a fixed size (min = max), so dwm floats it instead of
 * tiling it narrower than the frame, centred where the fbdev Doom draws.
 * Must never block on X while the engine expects a frame: events are read
 * only while XPending says some are queued.
 *
 * Built by scripts/x11-doom.sh into /opt/x11/bin/doom, which also sets
 * DOOM_WAD, the WAD used without -iwad. Docs:
 * docs/04-plan/waves/WAVE-16-x11-desktop.md.
 * ============================================================================= */
#define _POSIX_C_SOURCE 200809L
#include <ctype.h>
#include <stdint.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <time.h>

#include <X11/Xlib.h>
#include <X11/Xutil.h>
#include <X11/keysym.h>
#include <X11/extensions/XShm.h>
#include <sys/ipc.h>
#include <sys/shm.h>

#include "doomgeneric.h"
#include "doomkeys.h"

enum { KEYQ = 32, DEPTH_TRUECOLOR = 24, IMAGE_PAD_BITS = 32 };

static Display *dpy;
static Window win;
static GC gc;
static XImage *image;
static XShmSegmentInfo shminfo; /* MIT-SHM (W18-S04): the frame in a segment the server reads */
static int use_shm, shm_failed;
static Atom wm_delete;
static uint64_t start_ms;

/* a small ring of key events: Doom polls DG_GetKey until it returns 0 */
static struct { int pressed; unsigned char key; } keyq[KEYQ];
static unsigned kq_head, kq_tail;

static uint64_t now_ms(void) {
    struct timespec ts;
    clock_gettime(CLOCK_MONOTONIC, &ts);
    return (uint64_t)ts.tv_sec * 1000u + (uint64_t)ts.tv_nsec / 1000000u;
}

static void die(const char *why) {
    fprintf(stderr, "doom: %s\n", why);
    exit(1);
}

/* fix_size asks the window manager for exactly the frame's size. */
static void fix_size(void) {
    XSizeHints *hints = XAllocSizeHints();
    if (!hints)
        die("out of memory");
    hints->flags = PMinSize | PMaxSize | PPosition;
    hints->min_width = hints->max_width = DOOMGENERIC_RESX;
    hints->min_height = hints->max_height = DOOMGENERIC_RESY;
    XSetWMNormalHints(dpy, win, hints);
    XFree(hints);
}

static void name_window(void) {
    XClassHint class = {.res_name = "doom", .res_class = "Doom"};
    XSetClassHint(dpy, win, &class);
    XStoreName(dpy, win, "doom");
    wm_delete = XInternAtom(dpy, "WM_DELETE_WINDOW", False);
    XSetWMProtocols(dpy, win, &wm_delete, 1);
}

/* shm_refused notes the server refusing XShmAttach (BadAccess, say)
 * instead of letting Xlib's default handler end the program. */
static int shm_refused(Display *d, XErrorEvent *e) {
    (void)d;
    (void)e;
    shm_failed = 1;
    return 0;
}

/* shm_image makes the frame's image in a SysV segment the server attaches
 * too (W18-S04), or returns NULL when MIT-SHM is not there or fails. */
static XImage *shm_image(Visual *visual) {
    if (!XShmQueryExtension(dpy))
        return NULL;
    XImage *im = XShmCreateImage(dpy, visual, DEPTH_TRUECOLOR, ZPixmap, NULL, &shminfo, DOOMGENERIC_RESX,
                                 DOOMGENERIC_RESY);
    if (!im)
        return NULL;
    shminfo.shmid = shmget(IPC_PRIVATE, (size_t)im->bytes_per_line * im->height, IPC_CREAT | 0600);
    shminfo.shmaddr = im->data = shminfo.shmid < 0 ? (char *)-1 : shmat(shminfo.shmid, NULL, 0);
    shminfo.readOnly = False;
    if (im->data == (char *)-1) {
        XDestroyImage(im);
        return NULL;
    }
    XErrorHandler old = XSetErrorHandler(shm_refused);
    XShmAttach(dpy, &shminfo);
    XSync(dpy, False); /* the server has attached it, or refused */
    XSetErrorHandler(old);
    shmctl(shminfo.shmid, IPC_RMID, NULL); /* gone once both sides detach */
    if (shm_failed) {
        shmdt(shminfo.shmaddr);
        im->data = NULL;
        XDestroyImage(im);
        return NULL;
    }
    return im;
}

void DG_Init(void) {
    dpy = XOpenDisplay(NULL);
    if (!dpy)
        die("cannot open the X display");
    int screen = DefaultScreen(dpy);
    Visual *visual = DefaultVisual(dpy, screen);
    if (DefaultDepth(dpy, screen) != DEPTH_TRUECOLOR || visual->red_mask != 0xff0000)
        die("needs a 24-bit TrueColor screen (0xRRGGBB)");
    int x = (DisplayWidth(dpy, screen) - DOOMGENERIC_RESX) / 2;
    int y = (DisplayHeight(dpy, screen) - DOOMGENERIC_RESY) / 2;
    win = XCreateSimpleWindow(dpy, RootWindow(dpy, screen), x, y, DOOMGENERIC_RESX, DOOMGENERIC_RESY, 0,
                              BlackPixel(dpy, screen), BlackPixel(dpy, screen));
    fix_size();
    name_window();
    XSelectInput(dpy, win, StructureNotifyMask | KeyPressMask | KeyReleaseMask);
    XMapWindow(dpy, win);
    gc = XCreateGC(dpy, win, 0, NULL);
    for (XEvent e;;) {
        XNextEvent(dpy, &e);
        if (e.type == MapNotify)
            break;
    }
    image = shm_image(visual);
    use_shm = image != NULL;
    if (use_shm && image->bytes_per_line == DOOMGENERIC_RESX * 4) { /* Doom draws into the segment itself */
        free(DG_ScreenBuffer);
        DG_ScreenBuffer = (pixel_t *)image->data;
    }
    if (!image)
        image = XCreateImage(dpy, visual, DEPTH_TRUECOLOR, ZPixmap, 0, (char *)DG_ScreenBuffer, DOOMGENERIC_RESX,
                             DOOMGENERIC_RESY, IMAGE_PAD_BITS, 0);
    if (!image)
        die("XCreateImage failed");
    printf("doom: X window %dx%d at %d,%d\n", DOOMGENERIC_RESX, DOOMGENERIC_RESY, x, y);
    printf("doom: drawing %s\n", use_shm ? "through MIT-SHM" : "with XPutImage (no MIT-SHM)");
    fflush(stdout);
    start_ms = now_ms();
}

static unsigned char map_key(KeySym sym) {
    switch (sym) {
    case XK_Return:    return KEY_ENTER;
    case XK_Escape:    return KEY_ESCAPE;
    case XK_Tab:       return KEY_TAB;
    case XK_BackSpace: return KEY_BACKSPACE;
    case XK_Left:      return KEY_LEFTARROW;
    case XK_Right:     return KEY_RIGHTARROW;
    case XK_Up:        return KEY_UPARROW;
    case XK_Down:      return KEY_DOWNARROW;
    case XK_Control_L: case XK_Control_R: return KEY_FIRE;
    case XK_space:     return KEY_USE;
    case XK_Shift_L:   case XK_Shift_R:   return KEY_RSHIFT;
    case XK_Alt_L:     case XK_Alt_R:     return KEY_RALT;
    default:
        if (sym >= XK_F1 && sym <= XK_F12)
            return (unsigned char)(KEY_F1 + (sym - XK_F1));
        if (sym > XK_space && sym <= XK_asciitilde) /* printable keysyms are ASCII */
            return (unsigned char)tolower((int)sym);
        return 0;
    }
}

static void queue_key(int pressed, XKeyEvent *ev) {
    unsigned char k = map_key(XLookupKeysym(ev, 0));
    if (!k || (kq_head + 1) % KEYQ == kq_tail)
        return; /* unmapped, or the queue is full */
    keyq[kq_head].pressed = pressed;
    keyq[kq_head].key = k;
    kq_head = (kq_head + 1) % KEYQ;
}

/* autorepeat: X repeats a held key as a release and a press at one time. */
static int repeated(XKeyEvent *release) {
    if (!XEventsQueued(dpy, QueuedAfterReading))
        return 0;
    XEvent next;
    XPeekEvent(dpy, &next);
    if (next.type != KeyPress || next.xkey.keycode != release->keycode || next.xkey.time != release->time)
        return 0;
    XNextEvent(dpy, &next); /* drop the press too: the key is still down */
    return 1;
}

static void pump_events(void) {
    while (XPending(dpy) > 0) {
        XEvent e;
        XNextEvent(dpy, &e);
        if (e.type == KeyPress)
            queue_key(1, &e.xkey);
        else if (e.type == KeyRelease && !repeated(&e.xkey))
            queue_key(0, &e.xkey);
        else if (e.type == ClientMessage && (Atom)e.xclient.data.l[0] == wm_delete)
            exit(0);
    }
}

void DG_DrawFrame(void) {
    pump_events();
    if (!use_shm) {
        XPutImage(dpy, win, gc, image, 0, 0, 0, 0, DOOMGENERIC_RESX, DOOMGENERIC_RESY);
        XFlush(dpy);
        return;
    }
    if ((char *)DG_ScreenBuffer != image->data) /* rows padded: copy them into the segment */
        for (int y = 0; y < DOOMGENERIC_RESY; y++)
            memcpy(image->data + (size_t)y * image->bytes_per_line, DG_ScreenBuffer + (size_t)y * DOOMGENERIC_RESX,
                   DOOMGENERIC_RESX * 4);
    XShmPutImage(dpy, win, gc, image, 0, 0, 0, 0, DOOMGENERIC_RESX, DOOMGENERIC_RESY, False);
    XSync(dpy, False); /* the server has read the frame before the next one is written */
}

void DG_SleepMs(uint32_t ms) {
    struct timespec ts = {ms / 1000, (long)(ms % 1000) * 1000000L};
    nanosleep(&ts, NULL);
}

uint32_t DG_GetTicksMs(void) { return (uint32_t)(now_ms() - start_ms); }

int DG_GetKey(int *pressed, unsigned char *key) {
    pump_events();
    if (kq_tail == kq_head)
        return 0;
    *pressed = keyq[kq_tail].pressed;
    *key = keyq[kq_tail].key;
    kq_tail = (kq_tail + 1) % KEYQ;
    return 1;
}

void DG_SetWindowTitle(const char *title) {
    if (dpy && win) /* the engine may name the window before DG_Init */
        XStoreName(dpy, win, title);
}

/* with_wad adds "-iwad DOOM_WAD" unless one is given: the engine looks only
 * in "." (its config.h), and dmenu starts doom with no arguments. */
static char **with_wad(int *argc, char **argv) {
    for (int i = 1; i < *argc; i++)
        if (strcmp(argv[i], "-iwad") == 0)
            return argv;
    char **args = calloc((size_t)*argc + 3, sizeof *args);
    if (!args)
        die("out of memory");
    memcpy(args, argv, (size_t)*argc * sizeof *args);
    args[(*argc)++] = "-iwad";
    args[(*argc)++] = DOOM_WAD;
    return args;
}

int main(int argc, char **argv) {
    argv = with_wad(&argc, argv);
    doomgeneric_Create(argc, argv);
    for (;;)
        doomgeneric_Tick();
}
