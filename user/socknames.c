/* socknames.c - named Unix sockets and descriptor passing (W14-S07), the
 * sequel to socktest.c. Each check holds on Linux too: `make ports-smoke`
 * runs this binary on the host before it boots Burrow. A server and a
 * forked client talk over a path name in /tmp and over an abstract name;
 * a pipe end travels through SCM_RIGHTS; SO_PASSCRED brings
 * SCM_CREDENTIALS; the refusals (EADDRINUSE, ENOENT after unlink,
 * ECONNREFUSED, ENXIO, EINVAL) match. Prints "socknames: <name> ok" per
 * check, then "socknames: ok"; a failure prints FAIL. Never leaves its
 * socket node behind. Docs: docs/04-plan/waves/WAVE-14-desktop-groundwork.md. */
#define _GNU_SOURCE
#include <errno.h>
#include <fcntl.h>
#include <stddef.h>
#include <stdio.h>
#include <string.h>
#include <sys/socket.h>
#include <sys/stat.h>
#include <sys/un.h>
#include <sys/wait.h>
#include <unistd.h>

#define PATH "/tmp/socknames.sock"

static int failures;

static void check(const char *name, int good) {
    printf("socknames: %s %s\n", name, good ? "ok" : "FAIL");
    if (!good) failures++;
}

/* addr fills a sockaddr_un for name (a leading '@' means abstract) and
 * returns its length. */
static socklen_t addr(struct sockaddr_un *a, const char *name) {
    memset(a, 0, sizeof *a);
    a->sun_family = AF_UNIX;
    size_t n = strlen(name);
    memcpy(a->sun_path, name, n);
    if (name[0] == '@') {
        a->sun_path[0] = 0;
        return offsetof(struct sockaddr_un, sun_path) + n;
    }
    return offsetof(struct sockaddr_un, sun_path) + n + 1;
}

static int serve(const char *name) {
    struct sockaddr_un a;
    socklen_t l = addr(&a, name);
    int s = socket(AF_UNIX, SOCK_STREAM, 0);
    if (s < 0 || bind(s, (struct sockaddr *)&a, l) != 0 || listen(s, 4) != 0) return -1;
    return s;
}

static int dial(const char *name) {
    struct sockaddr_un a;
    socklen_t l = addr(&a, name);
    int c = socket(AF_UNIX, SOCK_STREAM, 0);
    if (connect(c, (struct sockaddr *)&a, l) != 0) {
        int e = errno;
        close(c);
        errno = e;
        return -1;
    }
    return c;
}

/* talk: a forked client says hello and reads the reply; the server checks
 * what it accepted (the peer's pid, the names). */
static void talk(const char *label, const char *name) {
    char n[64], b[32];
    int s = serve(name);
    snprintf(n, sizeof n, "%s-bind", label);
    check(n, s >= 0);
    pid_t kid = fork();
    if (kid == 0) {
        int c = dial(name);
        struct sockaddr_un peer;
        socklen_t pl = sizeof peer;
        int named = c >= 0 && getpeername(c, (struct sockaddr *)&peer, &pl) == 0 && pl == addr(&peer, name);
        if (!named || write(c, "hello", 5) != 5 || read(c, b, sizeof b) != 5 || memcmp(b, "howdy", 5) != 0) _exit(1);
        _exit(0);
    }
    struct sockaddr_un from;
    socklen_t fl = sizeof from;
    int a = accept(s, (struct sockaddr *)&from, &fl);
    struct ucred cr;
    socklen_t cl = sizeof cr;
    snprintf(n, sizeof n, "%s-accept", label);
    check(n, a >= 0 && fl == offsetof(struct sockaddr_un, sun_path) &&
                 getsockopt(a, SOL_SOCKET, SO_PEERCRED, &cr, &cl) == 0 && cr.pid == kid);
    snprintf(n, sizeof n, "%s-talk", label);
    int got = read(a, b, sizeof b);
    check(n, got == 5 && memcmp(b, "hello", 5) == 0 && write(a, "howdy", 5) == 5);
    int status;
    snprintf(n, sizeof n, "%s-client", label);
    check(n, waitpid(kid, &status, 0) == kid && WIFEXITED(status) && WEXITSTATUS(status) == 0);
    int again = serve(name);
    snprintf(n, sizeof n, "%s-inuse", label);
    check(n, again == -1 && errno == EADDRINUSE);
    close(a);
    close(s);
}

static void path_names(void) {
    struct stat st;
    unlink(PATH);
    talk("path", PATH);
    check("path-node", stat(PATH, &st) == 0 && S_ISSOCK(st.st_mode));
    check("path-open", open(PATH, O_RDONLY) == -1 && errno == ENXIO);
    check("path-refused", dial(PATH) == -1 && errno == ECONNREFUSED); /* the listener closed */
    unlink(PATH);
    check("path-unlinked", dial(PATH) == -1 && errno == ENOENT);
    close(open(PATH ".file", O_WRONLY | O_CREAT | O_TRUNC, 0644)); /* ours: Linux checks write permission first */
    check("path-file", dial(PATH ".file") == -1 && errno == ECONNREFUSED);
    unlink(PATH ".file");
}

static void abstract_names(void) {
    char name[48];
    snprintf(name, sizeof name, "@socknames-%d", getpid());
    talk("abstract", name);
    check("abstract-gone", dial(name) == -1 && errno == ECONNREFUSED);
    int s = socket(AF_UNIX, SOCK_STREAM, 0);
    struct sockaddr_un a = {.sun_family = AF_UNIX};
    socklen_t l = sizeof a;
    check("autobind", bind(s, (struct sockaddr *)&a, offsetof(struct sockaddr_un, sun_path)) == 0 &&
                          getsockname(s, (struct sockaddr *)&a, &l) == 0 && l == 8 && a.sun_path[0] == 0);
    int u = socket(AF_UNIX, SOCK_STREAM, 0);
    check("listen-unbound", listen(u, 1) == -1 && errno == EINVAL);
    check("accept-unlistened", accept(s, 0, 0) == -1 && errno == EINVAL);
    close(s);
    close(u);
}

/* pass sends one byte with fd attached (SCM_RIGHTS). */
static int pass(int sock, int fd) {
    char c = 'x';
    struct iovec iov = {&c, 1};
    union { struct cmsghdr h; char b[CMSG_SPACE(sizeof(int))]; } u;
    memset(&u, 0, sizeof u);
    struct msghdr m = {.msg_iov = &iov, .msg_iovlen = 1, .msg_control = u.b, .msg_controllen = sizeof u.b};
    struct cmsghdr *h = CMSG_FIRSTHDR(&m);
    h->cmsg_level = SOL_SOCKET;
    h->cmsg_type = SCM_RIGHTS;
    h->cmsg_len = CMSG_LEN(sizeof(int));
    memcpy(CMSG_DATA(h), &fd, sizeof fd);
    return sendmsg(sock, &m, 0);
}

static void descriptors(void) {
    int sv[2], p[2], one = 1;
    socketpair(AF_UNIX, SOCK_STREAM, 0, sv);
    pipe(p);
    check("rights-send", pass(sv[0], p[1]) == 1);
    close(p[1]);
    char c, buf[CMSG_SPACE(sizeof(int)) + CMSG_SPACE(sizeof(struct ucred))];
    struct iovec iov = {&c, 1};
    struct msghdr m = {.msg_iov = &iov, .msg_iovlen = 1, .msg_control = buf, .msg_controllen = sizeof buf};
    int fd = -1;
    if (recvmsg(sv[1], &m, 0) == 1 && CMSG_FIRSTHDR(&m) && CMSG_FIRSTHDR(&m)->cmsg_type == SCM_RIGHTS)
        memcpy(&fd, CMSG_DATA(CMSG_FIRSTHDR(&m)), sizeof fd);
    char got[8] = {0};
    check("rights-recv", fd >= 0 && write(fd, "piped", 5) == 5 && read(p[0], got, 5) == 5 && strcmp(got, "piped") == 0);
    pass(sv[0], p[0]);
    struct msghdr shortm = {.msg_iov = &iov, .msg_iovlen = 1, .msg_control = buf, .msg_controllen = sizeof(struct cmsghdr)};
    check("rights-ctrunc", recvmsg(sv[1], &shortm, 0) == 1 && (shortm.msg_flags & MSG_CTRUNC));
    setsockopt(sv[1], SOL_SOCKET, SO_PASSCRED, &one, sizeof one);
    write(sv[0], "c", 1);
    m.msg_controllen = sizeof buf;
    struct ucred cr = {0};
    if (recvmsg(sv[1], &m, 0) == 1)
        for (struct cmsghdr *h = CMSG_FIRSTHDR(&m); h; h = CMSG_NXTHDR(&m, h))
            if (h->cmsg_type == SCM_CREDENTIALS) memcpy(&cr, CMSG_DATA(h), sizeof cr);
    check("credentials", cr.pid == getpid() && cr.uid == getuid());
    close(sv[0]);
    close(sv[1]);
    close(fd);
    close(p[0]);
}

int main(void) {
    setvbuf(stdout, 0, _IONBF, 0);
    path_names();
    abstract_names();
    descriptors();
    if (failures) {
        printf("socknames: %d FAILED\n", failures);
        return 1;
    }
    printf("socknames: ok\n");
    return 0;
}
