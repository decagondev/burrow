/* xbench.c - how fast the X desktop is on Burrow (W16-S05, make x11-bench).
 * Run with no X server up: it times, in one boot,
 *   doom-fbdev-fps  Doom's timedemo demo1 on /dev/fb0 (the reference),
 *   server-ms       Xfbdev's exec to a client's first frame on the screen,
 *   st-ms           st's exec to its window mapped,
 *   st-ls-ms        st's exec, 100 lines from seq scrolled, st gone,
 *   doom-x11-fps    the same timedemo in an X window,
 * printing "xbench: <name> <value>" for tests/smoke/x11-budgets.py, then
 * stops the server. Never leaves a child behind on success: every program
 * it starts is waited for. Docs: docs/04-plan/waves/WAVE-16-x11-desktop.md. */
#define _POSIX_C_SOURCE 200809L
#include <X11/Xlib.h>
#include <X11/Xutil.h>
#include <fcntl.h>
#include <signal.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <sys/wait.h>
#include <time.h>
#include <unistd.h>

#define X11 "/opt/x11"
#define WAD X11 "/share/doom/freedoom1.wad"
enum { POLL_NS = 10000000, SERVER_TRIES = 3000, ST_TRIES = 3000 }; /* 10 ms looks, 30 s each */

static double now_ms(void) {
    struct timespec t;
    clock_gettime(CLOCK_MONOTONIC, &t);
    return t.tv_sec * 1e3 + t.tv_nsec / 1e6;
}

static void nap(void) {
    struct timespec t = {0, POLL_NS};
    nanosleep(&t, NULL);
}

static void fail(const char *why) {
    printf("xbench: FAIL %s\n", why);
    exit(1);
}

/* start forks argv; out >= 0 becomes the child's stdout and stderr. */
static pid_t start(char *const argv[], int out) {
    pid_t pid = fork();
    if (pid < 0)
        fail("fork");
    if (pid == 0) {
        if (out >= 0) {
            dup2(out, 1);
            dup2(out, 2);
        }
        execv(argv[0], argv);
        _exit(127);
    }
    return pid;
}

/* timedemo runs a Doom's timedemo and returns its "(F fps)", read from its output. */
static double timedemo(char *const argv[]) {
    int fds[2];
    if (pipe(fds) < 0)
        fail("pipe");
    pid_t pid = start(argv, fds[1]);
    close(fds[1]);
    char buf[256], line[512] = "";
    double fps = -1;
    ssize_t n;
    while ((n = read(fds[0], buf, sizeof buf - 1)) > 0) {
        buf[n] = 0;
        strncat(line, buf, sizeof line - strlen(line) - 1);
        char *p = strstr(line, " fps)");
        if (p && fps < 0) {
            while (p > line && p[-1] != '(')
                p--;
            fps = atof(p);
        }
        char *nl = strrchr(line, '\n'); /* keep only the unfinished line */
        if (nl)
            memmove(line, nl + 1, strlen(nl + 1) + 1);
    }
    close(fds[0]);
    waitpid(pid, NULL, 0);
    if (fps < 0)
        fail("no fps line from Doom");
    return fps;
}

/* first_frame waits for the server, then maps a window and returns once its
 * first fill has been drawn (XSync after the Expose). */
static Display *first_frame(void) {
    Display *d = NULL;
    for (int i = 0; i < SERVER_TRIES && !(d = XOpenDisplay(NULL)); i++)
        nap();
    if (!d)
        fail("the server never answered");
    Window w = XCreateSimpleWindow(d, DefaultRootWindow(d), 0, 0, 200, 100, 0, 0, 0xffffff);
    XSelectInput(d, w, ExposureMask);
    XMapWindow(d, w);
    for (XEvent e;;) {
        XNextEvent(d, &e);
        if (e.type == Expose)
            break;
    }
    XFillRectangle(d, w, DefaultGC(d, DefaultScreen(d)), 0, 0, 200, 100);
    XSync(d, False);
    XDestroyWindow(d, w);
    XSync(d, False);
    return d;
}

/* st_mapped says whether a top-level st window is viewable. */
static int st_mapped(Display *d) {
    Window root, parent, *kids = NULL;
    unsigned int n = 0;
    int found = 0;
    XQueryTree(d, DefaultRootWindow(d), &root, &parent, &kids, &n);
    for (unsigned int i = 0; i < n && !found; i++) {
        XClassHint c = {0};
        XWindowAttributes a;
        if (XGetClassHint(d, kids[i], &c) && c.res_class && strcmp(c.res_class, "st-256color") == 0)
            found = XGetWindowAttributes(d, kids[i], &a) && a.map_state == IsViewable;
        if (c.res_name)
            XFree(c.res_name);
        if (c.res_class)
            XFree(c.res_class);
    }
    if (kids)
        XFree(kids);
    return found;
}

static int ignore(Display *d, XErrorEvent *e) { /* windows may vanish mid-query */
    (void)d;
    (void)e;
    return 0;
}

/* time_st prints st-ms (exec to mapped) and st-ls-ms (exec to exit). */
static void time_st(Display *d) {
    char *st[] = {X11 "/bin/st", "-e", "/bin/busybox", "sh", "-c", "/bin/busybox seq 100", NULL};
    double t0 = now_ms();
    pid_t pid = start(st, -1);
    int i = 0;
    for (; i < ST_TRIES && !st_mapped(d); i++)
        nap();
    if (i == ST_TRIES)
        fail("st never mapped its window");
    printf("xbench: st-ms %.0f\n", now_ms() - t0);
    waitpid(pid, NULL, 0);
    printf("xbench: st-ls-ms %.0f\n", now_ms() - t0);
}

int main(void) {
    char *fbdoom[] = {X11 "/share/doom/doom-fbdev", "-iwad", WAD, "-timedemo", "demo1", NULL};
    printf("xbench: doom-fbdev-fps %.1f\n", timedemo(fbdoom));
    fflush(stdout);
    int log = open("/tmp/x.log", O_WRONLY | O_CREAT | O_TRUNC, 0644);
    char *server[] = {X11 "/bin/Xfbdev", ":0", "-nolock", NULL};
    double t0 = now_ms();
    pid_t x = start(server, log);
    Display *d = first_frame();
    printf("xbench: server-ms %.0f\n", now_ms() - t0);
    fflush(stdout);
    XSetErrorHandler(ignore);
    time_st(d);
    fflush(stdout);
    char *xdoom[] = {X11 "/bin/doom", "-timedemo", "demo1", NULL};
    printf("xbench: doom-x11-fps %.1f\n", timedemo(xdoom));
    XCloseDisplay(d);
    kill(x, SIGTERM);
    waitpid(x, NULL, 0);
    printf("xbench: done\n");
    return 0;
}
