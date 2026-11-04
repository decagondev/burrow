/* xfocus.c - wait for a window and give it the keyboard (W16-S03): waits
 * for a mapped top-level window whose WM_NAME or WM_CLASS is the argument,
 * then XSetInputFocus on it, as `xdotool search --sync ... windowfocus`
 * does on the host. Without a window manager TinyX's focus is PointerRoot,
 * so keys go to whatever is under the pointer: typed before a slow client
 * has mapped its window, they reach the root and are lost; xfocus's line
 * says when typing is safe. Under dwm (S04), which focuses windows itself,
 * -w only waits; -n N waits for the Nth such window, -t S for S seconds
 * (30). Prints "xfocus: <name>" ("xfocus: <name> #N" for N > 1), or
 * "xfocus: no window named <name>" and the windows there are (exit 1).
 * Docs: docs/04-plan/waves/WAVE-16-x11-desktop.md. */
#include <X11/Xlib.h>
#include <X11/Xutil.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <unistd.h>
#include <time.h>

enum { TRY_NS = 100000000, TRIES_PER_S = 10, DEFAULT_S = 30 }; /* a look every 0.1 s */

/* named: w's WM_NAME (its raw bytes, whatever the encoding) or either half
 * of its WM_CLASS is name. The class needs no locale data: st's title goes
 * through Xutf8TextListToTextProperty, which fails without it on Burrow. */
static int named(Display *d, Window w, const char *name) {
    int match = 0;
    XTextProperty title = {0};
    if (XGetWMName(d, w, &title) && title.value)
        match = title.nitems == strlen(name) && memcmp(title.value, name, title.nitems) == 0;
    if (title.value)
        XFree(title.value);
    XClassHint class = {0};
    if (!match && XGetClassHint(d, w, &class))
        match = (class.res_name && strcmp(class.res_name, name) == 0) ||
                (class.res_class && strcmp(class.res_class, name) == 0);
    if (class.res_name)
        XFree(class.res_name);
    if (class.res_class)
        XFree(class.res_class);
    return match;
}

/* gone: a window can be destroyed between XQueryTree and the questions about
 * it (W16-S05: a dmenu closing), and Xlib's default handler would exit on
 * that BadWindow. Such a window is simply not the one wanted. */
static int gone(Display *d, XErrorEvent *e) {
    (void)d;
    (void)e;
    return 0;
}

/* find returns the newest of the mapped windows named name once there are
 * at least count of them (stacking order: the last child is the newest). */
static Window find(Display *d, const char *name, int count) {
    Window root, parent, *kids = NULL, found = 0;
    unsigned int n = 0;
    int seen = 0;
    if (!XQueryTree(d, DefaultRootWindow(d), &root, &parent, &kids, &n))
        return 0;
    for (unsigned int i = 0; i < n; i++) {
        XWindowAttributes a;
        if (named(d, kids[i], name) && XGetWindowAttributes(d, kids[i], &a) && a.map_state == IsViewable) {
            found = kids[i];
            seen++;
        }
    }
    if (seen < count)
        found = 0;
    if (kids)
        XFree(kids);
    return found;
}

/* list prints the top-level windows there are, to show what was not matched. */
static void list(Display *d) {
    Window root, parent, *kids = NULL;
    unsigned int n = 0;
    XQueryTree(d, DefaultRootWindow(d), &root, &parent, &kids, &n);
    for (unsigned int i = 0; i < n; i++) {
        XTextProperty title = {0};
        XClassHint class = {0};
        XWindowAttributes a = {0};
        XGetWMName(d, kids[i], &title);
        XGetClassHint(d, kids[i], &class);
        XGetWindowAttributes(d, kids[i], &a);
        printf("xfocus: window 0x%lx map %d name \"%.*s\" class \"%s\" \"%s\"\n", kids[i], a.map_state,
               title.value ? (int)title.nitems : 0, title.value ? (char *)title.value : "",
               class.res_name ? class.res_name : "", class.res_class ? class.res_class : "");
    }
}

int main(int argc, char **argv) {
    int wait_only = 0, count = 1, seconds = DEFAULT_S, opt;
    while ((opt = getopt(argc, argv, "wn:t:")) != -1) {
        if (opt == 'w')
            wait_only = 1;
        else if (opt == 'n')
            count = atoi(optarg);
        else if (opt == 't')
            seconds = atoi(optarg);
        else
            optind = argc + 1;
    }
    if (optind != argc - 1 || count < 1 || seconds < 1) {
        fprintf(stderr, "usage: xfocus [-w] [-n count] [-t seconds] <window name or class>\n");
        return 2;
    }
    const char *name = argv[optind];
    Display *d = XOpenDisplay(NULL);
    if (!d) {
        printf("xfocus: cannot open display\n");
        return 1;
    }
    XSetErrorHandler(gone);
    struct timespec pause = {0, TRY_NS};
    for (int i = 0; i < seconds * TRIES_PER_S; i++) {
        Window w = find(d, name, count);
        if (w) {
            if (!wait_only) {
                XSetInputFocus(d, w, RevertToPointerRoot, CurrentTime);
                XSync(d, False);
            }
            if (count > 1)
                printf("xfocus: %s #%d\n", name, count);
            else
                printf("xfocus: %s\n", name);
            return 0;
        }
        nanosleep(&pause, NULL);
    }
    printf("xfocus: no window named %s\n", name);
    list(d);
    return 1;
}
