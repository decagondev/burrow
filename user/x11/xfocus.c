/* xfocus.c - wait for a window and give it the keyboard (W16-S03): waits
 * up to thirty seconds for a mapped top-level window whose WM_NAME or
 * WM_CLASS is the argument, then XSetInputFocus on it, as `xdotool search --sync ...
 * windowfocus` does on the host. Without a window manager TinyX's focus is
 * PointerRoot, so keys go to whatever is under the pointer: typed before a
 * slow client has mapped its window, they reach the root and are lost;
 * xfocus's line says when typing is safe. dwm (S04) focuses windows itself.
 * Prints "xfocus: <name>", or "xfocus: no window named <name>" (exit 1).
 * Docs: docs/04-plan/waves/WAVE-16-x11-desktop.md. */
#include <X11/Xlib.h>
#include <X11/Xutil.h>
#include <stdio.h>
#include <string.h>
#include <time.h>

enum { TRIES = 300, TRY_NS = 100000000 }; /* 300 x 0.1 s */

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

static Window find(Display *d, const char *name) {
    Window root, parent, *kids = NULL, found = 0;
    unsigned int n = 0;
    if (!XQueryTree(d, DefaultRootWindow(d), &root, &parent, &kids, &n))
        return 0;
    for (unsigned int i = 0; i < n && !found; i++) {
        XWindowAttributes a;
        if (named(d, kids[i], name) && XGetWindowAttributes(d, kids[i], &a) && a.map_state == IsViewable)
            found = kids[i];
    }
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
    if (argc != 2) {
        fprintf(stderr, "usage: xfocus <window name>\n");
        return 2;
    }
    Display *d = XOpenDisplay(NULL);
    if (!d) {
        printf("xfocus: cannot open display\n");
        return 1;
    }
    struct timespec pause = {0, TRY_NS};
    for (int i = 0; i < TRIES; i++) {
        Window w = find(d, argv[1]);
        if (w) {
            XSetInputFocus(d, w, RevertToPointerRoot, CurrentTime);
            XSync(d, False);
            printf("xfocus: %s\n", argv[1]);
            return 0;
        }
        nanosleep(&pause, NULL);
    }
    printf("xfocus: no window named %s\n", argv[1]);
    list(d);
    return 1;
}
