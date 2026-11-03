// ptytest.c - a pseudo-terminal used the way st uses one (W16-S03): musl's
// openpty with a window size, a child that makes the slave its controlling
// terminal (setsid, TIOCSCTTY) and talks through it, and the parent as the
// terminal emulator on the master. Checks the size, CR NL output (ONLCR),
// /dev/tty, echo, a canonical line, ^C as SIGINT, and EIO on the master
// once the child is gone. Never touches the console. The same binary runs
// on the host's Linux first (make ports-smoke), then on Burrow.
// Docs: docs/04-plan/waves/WAVE-16-x11-desktop.md.
#include <errno.h>
#include <fcntl.h>
#include <poll.h>
#include <pty.h>
#include <signal.h>
#include <stdio.h>
#include <string.h>
#include <sys/ioctl.h>
#include <sys/wait.h>
#include <unistd.h>

enum { ROWS = 30, COLS = 100, WAIT_MS = 3000, SIGINT_EXIT = 7 };

static int fails;

static void check(int ok, const char *what) {
    printf("ptytest: %s %s\n", what, ok ? "ok" : "FAIL");
    fflush(stdout);
    if (!ok)
        fails++;
}

// expect reads the master until want has appeared (what follows is kept).
static int expect(int m, const char *want) {
    static char acc[4096];
    static size_t len;
    for (;;) {
        acc[len] = 0;
        char *at = strstr(acc, want);
        if (at) {
            size_t used = (size_t)(at - acc) + strlen(want);
            memmove(acc, acc + used, len - used);
            len -= used;
            return 1;
        }
        struct pollfd pf = {m, POLLIN, 0};
        if (poll(&pf, 1, WAIT_MS) <= 0 || len + 1 >= sizeof acc)
            return 0;
        ssize_t n = read(m, acc + len, sizeof acc - 1 - len);
        if (n <= 0)
            return 0;
        len += (size_t)n;
    }
}

static void on_int(int sig) {
    (void)sig;
    static const char msg[] = "child: sigint\n";
    write(1, msg, sizeof msg - 1);
    _exit(SIGINT_EXIT);
}

static void child(int s) {
    setsid();
    if (ioctl(s, TIOCSCTTY, 0) < 0)
        _exit(10);
    dup2(s, 0);
    dup2(s, 1);
    dup2(s, 2);
    if (s > 2)
        close(s);
    signal(SIGINT, on_int);
    struct winsize ws;
    ioctl(0, TIOCGWINSZ, &ws);
    dprintf(1, "child: %dx%d\n", ws.ws_row, ws.ws_col);
    int t = open("/dev/tty", O_RDWR);
    dprintf(t >= 0 ? t : 1, "child: tty %s\n", t >= 0 ? "ok" : "missing");
    char line[64];
    ssize_t n = read(0, line, sizeof line - 1);
    line[n > 0 ? n : 0] = 0;
    dprintf(1, "child: got %s", line);
    for (;;)
        pause();
}

int main(void) {
    int m, s;
    struct winsize ws = {.ws_row = ROWS, .ws_col = COLS};
    if (openpty(&m, &s, NULL, NULL, &ws) < 0) {
        printf("ptytest: openpty FAIL (errno %d)\n", errno);
        return 1;
    }
    pid_t pid = fork();
    if (pid == 0) {
        close(m);
        child(s);
    }
    close(s);
    check(expect(m, "child: 30x100\r\n"), "winsize-onlcr");
    check(expect(m, "child: tty ok\r\n"), "dev-tty");
    write(m, "abc\n", 4);
    check(expect(m, "abc\r\n"), "echo");
    check(expect(m, "child: got abc\r\n"), "canonical-line");
    write(m, "\x03", 1);
    check(expect(m, "child: sigint\r\n"), "sigint");
    int status = 0;
    waitpid(pid, &status, 0);
    check(WIFEXITED(status) && WEXITSTATUS(status) == SIGINT_EXIT, "exit");
    char b[256];
    ssize_t n;
    while ((n = read(m, b, sizeof b)) > 0)
        ;
    check(n < 0 && errno == EIO, "eio");
    printf("ptytest: %s\n", fails ? "FAIL" : "ok");
    return fails != 0;
}
