/* socktest.c - Unix socket pairs (W14-S06). Each check holds on Linux too:
 * `make ports-smoke` runs this binary on the host before it boots Burrow.
 * Covers socketpair's types and flags, stream joins and datagram
 * boundaries, the send/recv flags, sendmsg/recvmsg, shutdown and close
 * (end of file, EPIPE, SIGPIPE), readiness for poll, select and epoll,
 * the socket options, fstat and FIONREAD. Prints "socktest: <name> ok"
 * per check, then "socktest: ok"; a failed check prints FAIL. Never
 * checks values that differ between kernels (buffer limits beyond the
 * defaults' doubling rule). Docs: docs/04-plan/waves/WAVE-14-desktop-groundwork.md. */
#define _GNU_SOURCE
#include <errno.h>
#include <fcntl.h>
#include <poll.h>
#include <signal.h>
#include <stdio.h>
#include <string.h>
#include <sys/epoll.h>
#include <sys/ioctl.h>
#include <sys/select.h>
#include <sys/socket.h>
#include <sys/stat.h>
#include <sys/uio.h>
#include <sys/wait.h>
#include <unistd.h>

static int failures;
static volatile sig_atomic_t pipes;

static void check(const char *name, int good) {
    printf("socktest: %s %s\n", name, good ? "ok" : "FAIL");
    if (!good) failures++;
}

static void on_pipe(int sig) { (void)sig; pipes++; }

static void pair(int type, int sv[2]) {
    if (socketpair(AF_UNIX, type, 0, sv) != 0) { perror("socketpair"); _exit(1); }
}

static void streams(void) {
    int sv[2];
    char b[64];
    pair(SOCK_STREAM, sv);
    write(sv[0], "hello", 5);
    send(sv[0], "hel", 3, 0);
    check("peek", recv(sv[1], b, 3, MSG_PEEK) == 3 && memcmp(b, "hel", 3) == 0);
    check("stream-joins", read(sv[1], b, sizeof b) == 8 && memcmp(b, "hellohel", 8) == 0);
    check("dontwait", recv(sv[1], b, 1, MSG_DONTWAIT) == -1 && errno == EAGAIN);
    struct iovec iov[2] = {{"ab", 2}, {"cd", 2}};
    check("writev-readv", writev(sv[0], iov, 2) == 4 && read(sv[1], b, 4) == 4 && memcmp(b, "abcd", 4) == 0);
    pid_t kid = fork();
    if (kid == 0) {
        write(sv[0], "one", 3);
        usleep(20000);
        write(sv[0], "two", 3);
        _exit(0);
    }
    check("waitall", recv(sv[1], b, 6, MSG_WAITALL) == 6 && memcmp(b, "onetwo", 6) == 0);
    waitpid(kid, 0, 0);
    shutdown(sv[0], SHUT_WR);
    check("shut-wr-eof", read(sv[1], b, 1) == 0);
    check("shut-wr-epipe", send(sv[0], "x", 1, MSG_NOSIGNAL) == -1 && errno == EPIPE);
    signal(SIGPIPE, on_pipe);
    check("sigpipe", write(sv[0], "x", 1) == -1 && errno == EPIPE && pipes == 1);
    close(sv[0]);
    close(sv[1]);
}

static void datagrams(void) {
    int types[] = {SOCK_DGRAM, SOCK_SEQPACKET};
    const char *names[] = {"dgram", "seqpacket"};
    char b[64], name[48];
    for (int i = 0; i < 2; i++) {
        int sv[2];
        pair(types[i], sv);
        send(sv[0], "first", 5, 0);
        send(sv[0], "first message", 13, 0);
        snprintf(name, sizeof name, "%s-boundaries", names[i]);
        check(name, recv(sv[1], b, sizeof b, 0) == 5);
        snprintf(name, sizeof name, "%s-trunc", names[i]);
        check(name, recv(sv[1], b, 3, MSG_TRUNC) == 13 && recv(sv[1], b, 3, MSG_DONTWAIT) == -1 && errno == EAGAIN);
        send(sv[0], "0123456789", 10, 0);
        char x[4], y[2];
        struct iovec iov[2] = {{x, 4}, {y, 2}};
        struct msghdr m = {.msg_iov = iov, .msg_iovlen = 2};
        snprintf(name, sizeof name, "%s-recvmsg", names[i]);
        check(name, recvmsg(sv[1], &m, 0) == 6 && memcmp(x, "0123", 4) == 0 && memcmp(y, "45", 2) == 0 &&
                        (m.msg_flags & MSG_TRUNC));
        close(sv[0]);
        close(sv[1]);
    }
}

static void readiness(void) {
    int sv[2];
    char b[8];
    pair(SOCK_STREAM | SOCK_NONBLOCK | SOCK_CLOEXEC, sv);
    check("nonblock", (fcntl(sv[0], F_GETFL) & O_NONBLOCK) && read(sv[0], b, 1) == -1 && errno == EAGAIN);
    check("cloexec", fcntl(sv[1], F_GETFD) & FD_CLOEXEC);
    struct pollfd p[2] = {{sv[1], POLLIN | POLLRDHUP, 0}, {sv[0], POLLOUT, 0}};
    check("poll-idle", poll(p, 2, 0) == 1 && p[0].revents == 0 && p[1].revents == POLLOUT);
    write(sv[0], "abc", 3);
    int n = 0;
    check("fionread", ioctl(sv[1], FIONREAD, &n) == 0 && n == 3);
    fd_set r;
    FD_ZERO(&r);
    FD_SET(sv[1], &r);
    struct timeval zero = {0, 0};
    check("select", select(sv[1] + 1, &r, 0, 0, &zero) == 1 && FD_ISSET(sv[1], &r));
    int ep = epoll_create1(0);
    struct epoll_event ev = {.events = EPOLLIN | EPOLLRDHUP, .data.fd = sv[1]}, got;
    epoll_ctl(ep, EPOLL_CTL_ADD, sv[1], &ev);
    read(sv[1], b, 3);
    close(sv[0]);
    check("poll-hup", poll(p, 1, 0) == 1 && p[0].revents == (POLLIN | POLLHUP | POLLRDHUP));
    check("epoll-rdhup", epoll_wait(ep, &got, 1, 0) == 1 && (got.events & EPOLLRDHUP) && (got.events & EPOLLHUP));
    check("closed-eof", read(sv[1], b, 1) == 0);
    close(ep);
    close(sv[1]);
}

static int opt(int fd, int name) {
    int v = -1;
    socklen_t l = sizeof v;
    return getsockopt(fd, SOL_SOCKET, name, &v, &l) == 0 && l == sizeof v ? v : -1;
}

static void options(void) {
    int sv[2], p[2];
    pair(SOCK_DGRAM, sv);
    check("so_type", opt(sv[0], SO_TYPE) == SOCK_DGRAM && opt(sv[0], SO_ERROR) == 0);
    check("so_sndbuf", opt(sv[0], SO_SNDBUF) > 0 && opt(sv[0], SO_RCVBUF) > 0);
    int v = 4096;
    check("so_rcvbuf-doubled", setsockopt(sv[0], SOL_SOCKET, SO_RCVBUF, &v, sizeof v) == 0 && opt(sv[0], SO_RCVBUF) == 8192);
    struct stat st;
    check("fstat-sock", fstat(sv[0], &st) == 0 && S_ISSOCK(st.st_mode));
    pipe(p);
    check("enotsock", shutdown(p[0], SHUT_RD) == -1 && errno == ENOTSOCK);
    int raw[2];
    check("raw-is-dgram", socketpair(AF_UNIX, SOCK_RAW, 0, raw) == 0 && opt(raw[0], SO_TYPE) == SOCK_DGRAM);
    check("inet-pair", socketpair(AF_INET, SOCK_STREAM, 0, raw) == -1 && errno == EOPNOTSUPP);
}

int main(void) {
    setvbuf(stdout, 0, _IONBF, 0);
    streams();
    datagrams();
    readiness();
    options();
    if (failures) {
        printf("socktest: %d FAILED\n", failures);
        return 1;
    }
    printf("socktest: ok\n");
    return 0;
}
