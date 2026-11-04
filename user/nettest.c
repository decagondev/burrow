/* nettest.c - AF_INET and AF_INET6 sockets over loopback (W17-S03). Each
 * check holds on Linux too: `make ports-smoke` runs this binary on the host
 * before it boots Burrow. Covers TCP (connect, accept, both directions, the
 * names, shutdown, a non-blocking connect with poll and SO_ERROR, a refused
 * port, readiness, EPIPE), UDP (sendto/recvfrom, MSG_TRUNC) and IPv6.
 * Every port is chosen by the kernel (bind to port 0). Prints
 * "nettest: <name> ok" per check, then a measurement (16 MiB between two
 * processes, and the slowest getppid while it flows: PIT-47) and
 * "nettest: ok"; a failed check prints FAIL. Never needs a network card.
 * Docs: docs/04-plan/waves/WAVE-17-networking.md. */
#define _GNU_SOURCE
#include <arpa/inet.h>
#include <errno.h>
#include <fcntl.h>
#include <netinet/in.h>
#include <netinet/tcp.h>
#include <poll.h>
#include <stdio.h>
#include <string.h>
#include <sys/socket.h>
#include <sys/wait.h>
#include <time.h>
#include <unistd.h>

enum { BULK = 16 << 20, CHUNK = 64 << 10 };

static int failures;

static void check(const char *name, int good) {
    printf("nettest: %s %s\n", name, good ? "ok" : "FAIL");
    if (!good) failures++;
}

static struct sockaddr_in loop4(int port) {
    struct sockaddr_in a = {.sin_family = AF_INET, .sin_port = htons(port)};
    a.sin_addr.s_addr = htonl(INADDR_LOOPBACK);
    return a;
}

/* listener listens on 127.0.0.1 at a port the kernel picks and returns it. */
static int listener(int *port) {
    int l = socket(AF_INET, SOCK_STREAM, 0), one = 1;
    struct sockaddr_in a = loop4(0);
    socklen_t n = sizeof a;
    setsockopt(l, SOL_SOCKET, SO_REUSEADDR, &one, sizeof one);
    if (bind(l, (void *)&a, sizeof a) || listen(l, 8) || getsockname(l, (void *)&a, &n)) {
        perror("nettest: listener");
        _exit(1);
    }
    *port = ntohs(a.sin_port);
    return l;
}

static int dial(int port, int flags) {
    int c = socket(AF_INET, SOCK_STREAM | flags, 0);
    struct sockaddr_in a = loop4(port);
    if (connect(c, (void *)&a, sizeof a) && errno != EINPROGRESS) return -errno;
    return c;
}

static void tcp(void) {
    int port, l = listener(&port), c = dial(port, 0);
    struct sockaddr_in peer;
    socklen_t n = sizeof peer;
    int s = accept(l, (void *)&peer, &n);
    char buf[64] = {0};
    check("tcp-connect-accept", c >= 0 && s >= 0 && n == sizeof peer && peer.sin_addr.s_addr == htonl(INADDR_LOOPBACK));
    struct pollfd pf = {.fd = s, .events = POLLIN};
    check("poll-empty", poll(&pf, 1, 0) == 0);
    write(c, "ping", 4);
    check("poll-readable", poll(&pf, 1, 1000) == 1 && (pf.revents & POLLIN));
    check("tcp-client-to-server", read(s, buf, sizeof buf) == 4 && memcmp(buf, "ping", 4) == 0);
    write(s, "pong!", 5);
    check("tcp-server-to-client", read(c, buf, sizeof buf) == 5 && memcmp(buf, "pong!", 5) == 0);
    struct sockaddr_in me, them;
    socklen_t a = sizeof me, b = sizeof them;
    getsockname(c, (void *)&me, &a);
    getpeername(s, (void *)&them, &b);
    check("names", me.sin_port == them.sin_port && peer.sin_port == me.sin_port);
    int one = 1, v = 0;
    socklen_t vl = sizeof v;
    setsockopt(c, IPPROTO_TCP, TCP_NODELAY, &one, sizeof one);
    getsockopt(c, IPPROTO_TCP, TCP_NODELAY, &v, &vl);
    check("tcp-nodelay", v == 1);
    shutdown(c, SHUT_WR);
    check("shutdown-eof", read(s, buf, sizeof buf) == 0);
    write(s, "late", 4);
    check("half-open", read(c, buf, sizeof buf) == 4);
    close(s);
    usleep(100000); /* the reset reaches c */
    ssize_t w1 = send(c, "x", 1, MSG_NOSIGNAL), w2 = send(c, "x", 1, MSG_NOSIGNAL);
    check("epipe", (w1 < 0 || w2 < 0) && (errno == EPIPE || errno == ECONNRESET));
    close(c);
    close(l);
}

static void connects(void) {
    int port, l = listener(&port);
    int c = dial(port, SOCK_NONBLOCK), err = -1;
    socklen_t el = sizeof err;
    struct pollfd pf = {.fd = c, .events = POLLOUT};
    check("nonblock-connect", c >= 0 && poll(&pf, 1, 2000) == 1 &&
          getsockopt(c, SOL_SOCKET, SO_ERROR, &err, &el) == 0 && err == 0);
    int s = accept4(l, NULL, NULL, SOCK_NONBLOCK);
    char b;
    check("accept4-nonblock", s >= 0 && read(s, &b, 1) < 0 && errno == EAGAIN);
    close(s);
    close(c);
    close(l);
    l = listener(&port);
    close(l); /* nobody listens there now */
    check("refused", dial(port, 0) == -ECONNREFUSED);
    c = dial(port, SOCK_NONBLOCK);
    pf.fd = c;
    err = 0;
    int ready = poll(&pf, 1, 2000);
    getsockopt(c, SOL_SOCKET, SO_ERROR, &err, &el);
    check("refused-nonblock", c == -ECONNREFUSED || (ready == 1 && err == ECONNREFUSED));
    if (c >= 0) close(c);
}

static void udp(void) {
    int u = socket(AF_INET, SOCK_DGRAM, 0), v = socket(AF_INET, SOCK_DGRAM, 0);
    struct sockaddr_in a = loop4(0), from;
    socklen_t n = sizeof a, fl = sizeof from;
    bind(u, (void *)&a, sizeof a);
    getsockname(u, (void *)&a, &n);
    sendto(v, "hello udp", 9, 0, (void *)&a, sizeof a);
    char buf[16];
    ssize_t got = recvfrom(u, buf, 5, MSG_TRUNC, (void *)&from, &fl);
    check("udp-sendto-recvfrom", got == 9 && memcmp(buf, "hello", 5) == 0 && from.sin_addr.s_addr == htonl(INADDR_LOOPBACK));
    int u2 = socket(AF_INET, SOCK_DGRAM, 0);
    check("udp-addrinuse", bind(u2, (void *)&a, sizeof a) < 0 && errno == EADDRINUSE);
    check("udp-dontwait", recv(u, buf, sizeof buf, MSG_DONTWAIT) < 0 && errno == EAGAIN);
    close(u);
    close(v);
    close(u2);
}

static void ipv6(void) {
    int l = socket(AF_INET6, SOCK_STREAM, 0), c = socket(AF_INET6, SOCK_STREAM, 0);
    struct sockaddr_in6 a = {.sin6_family = AF_INET6, .sin6_addr = IN6ADDR_LOOPBACK_INIT};
    socklen_t n = sizeof a;
    bind(l, (void *)&a, sizeof a);
    listen(l, 1);
    getsockname(l, (void *)&a, &n);
    int ok = connect(c, (void *)&a, sizeof a) == 0;
    int s = accept(l, NULL, NULL);
    char b[2] = {0};
    write(c, "6", 1);
    check("ipv6-loopback", ok && s >= 0 && read(s, b, 1) == 1 && b[0] == '6');
    close(s);
    close(c);
    close(l);
}

static double now_ms(void) {
    struct timespec t;
    clock_gettime(CLOCK_MONOTONIC, &t);
    return t.tv_sec * 1e3 + t.tv_nsec / 1e6;
}

/* bulk sends BULK bytes from a child to this process and reports the time
 * and the slowest getppid seen meanwhile (is the dispatcher starved?). */
static void bulk(void) {
    static char buf[CHUNK];
    int port, l = listener(&port);
    pid_t pid = fork();
    if (pid == 0) {
        int c = dial(port, 0);
        for (int sent = 0; sent < BULK;) {
            ssize_t k = write(c, buf, CHUNK);
            if (k <= 0) _exit(1);
            sent += k;
        }
        _exit(0);
    }
    int s = accept(l, NULL, NULL);
    double start = now_ms(), slowest = 0;
    long total = 0;
    ssize_t k;
    while ((k = read(s, buf, sizeof buf)) > 0) {
        total += k;
        double t0 = now_ms();
        getppid();
        double d = now_ms() - t0;
        if (d > slowest) slowest = d;
    }
    double ms = now_ms() - start;
    int status = 1;
    waitpid(pid, &status, 0);
    check("bulk", total == BULK && WIFEXITED(status) && WEXITSTATUS(status) == 0);
    printf("nettest: %d MiB over loopback in %.0f ms (%.1f MB/s), slowest getppid %.0f us\n",
           BULK >> 20, ms, BULK / 1e3 / (ms > 0 ? ms : 1), slowest * 1e3);
    close(s);
    close(l);
}

int main(void) {
    setvbuf(stdout, NULL, _IONBF, 0);
    tcp();
    connects();
    udp();
    ipv6();
    bulk();
    printf("nettest: %s\n", failures ? "FAIL" : "ok");
    return failures != 0;
}
