/* credtest.c - user and group ids (W19-S03): the set*id rules for root and
 * for a user, supplementary groups, the file-system ids, kill's permission
 * check, SO_PEERCRED carrying the connecting process's effective ids, and
 * AT_SECURE. Each case runs in a child, which gives up root as it pleases.
 * With --exec [program] (as root) also a set-user-id and set-group-id copy
 * of program (default /proc/self/exe; Burrow, which has no /proc, names
 * /bin/credtest), run by another user: on Linux since S03, on Burrow since
 * S04 (chown).
 * Prints "credtest: <case> ok|FAIL", then "credtest: ok" or "credtest: FAIL".
 * Must run as root. Docs: docs/04-plan/waves/WAVE-19-unix.md (S03). */
#define _GNU_SOURCE
#include <errno.h>
#include <stddef.h>
#include <grp.h>
#include <signal.h>
#include <stdio.h>
#include <string.h>
#include <sys/auxv.h>
#include <sys/fsuid.h>
#include <sys/socket.h>
#include <sys/stat.h>
#include <sys/un.h>
#include <sys/wait.h>
#include <unistd.h>

static int failures;

static void check(const char *name, int ok) {
    printf("credtest: %s %s\n", name, ok ? "ok" : "FAIL");
    failures += !ok;
}

static int resuid(uid_t r, uid_t e, uid_t s) {
    uid_t a, b, c;
    return getresuid(&a, &b, &c) == 0 && a == r && b == e && c == s;
}

static int resgid(gid_t r, gid_t e, gid_t s) {
    gid_t a, b, c;
    return getresgid(&a, &b, &c) == 0 && a == r && b == e && c == s;
}

static int eperm(int r) { return r == -1 && errno == EPERM; }

/* each case returns 0, or the line that went wrong */
#define NEED(x) do { if (!(x)) return __LINE__; } while (0)

static int drop_for_good(void) {
    NEED(setgid(100) == 0 && setuid(1000) == 0);
    NEED(resuid(1000, 1000, 1000) && resgid(100, 100, 100));
    NEED(eperm(setuid(0)) && eperm(seteuid(0)) && eperm(setgroups(0, NULL)));
    return 0;
}

static int temporary_drop(void) {
    NEED(seteuid(1000) == 0 && resuid(0, 1000, 0) && geteuid() == 1000);
    NEED(seteuid(0) == 0 && resuid(0, 0, 0));
    return 0;
}

static int set_res(void) {
    NEED(setresgid(7, 8, 9) == 0 && resgid(7, 8, 9));
    NEED(setresuid(1000, 1001, 1002) == 0 && resuid(1000, 1001, 1002));
    NEED(setresuid(1001, 1002, 1000) == 0 && resuid(1001, 1002, 1000)); /* a permutation is allowed */
    NEED(eperm(setresuid(5, -1, -1)) && resuid(1001, 1002, 1000));
    NEED(setuid(1000) == 0 && resuid(1001, 1000, 1000)); /* a user's setuid sets the effective id only */
    NEED(eperm(setuid(1003)) && eperm(setresgid(1, 1, 1))); /* 7 would do: it is one of ours */
    return 0;
}

static int set_re(void) {
    NEED(setreuid(1000, 1001) == 0 && resuid(1000, 1001, 1001));      /* a new real id: saved = effective */
    NEED(setreuid(-1, 1000) == 0 && resuid(1000, 1000, 1001));        /* effective = real: saved stays */
    NEED(eperm(setreuid(1001, -1)) && resuid(1000, 1000, 1001));      /* real only to real or effective */
    NEED(setreuid(-1, 1001) == 0 && resuid(1000, 1001, 1001));        /* effective to the saved id */
    return 0;
}

static int fs_ids(void) {
    NEED(setfsuid(1234) == 0 && setfsuid(-1) == 1234); /* -1 only asks */
    NEED(seteuid(1000) == 0 && setfsuid(-1) == 1000); /* the effective id carries it along */
    NEED(setfsuid(77) == 1000 && setfsuid(-1) == 1000); /* a user may not pick one */
    NEED(setfsgid(55) == 0 && setfsgid(-1) == 0);      /* still root's: not a user's group */
    return 0;
}

static int kill_rules(void) {
    pid_t parent = getppid();
    NEED(setuid(1000) == 0);
    NEED(kill(getpid(), 0) == 0);
    NEED(eperm(kill(parent, 0)) && eperm(kill(parent, SIGTERM))); /* root's */
    return 0;
}

static void groups(void) {
    gid_t set[2] = {10, 20}, got[8];
    int ok = setgroups(2, set) == 0 && getgroups(0, NULL) == 2 && getgroups(8, got) == 2 &&
             got[0] == 10 && got[1] == 20 && getgroups(1, got) == -1 && errno == EINVAL;
    check("groups", ok && setgroups(0, NULL) == 0 && getgroups(0, NULL) == 0);
}

static void in_child(const char *name, int (*fn)(void)) {
    pid_t pid = fork();
    if (pid == 0)
        _exit(fn() ? 1 : 0);
    int st;
    waitpid(pid, &st, 0);
    if (!WIFEXITED(st) || WEXITSTATUS(st) != 0) { /* run it again here to say where */
        pid = fork();
        if (pid == 0) {
            printf("credtest: %s went wrong at line %d\n", name, fn());
            _exit(0);
        }
        waitpid(pid, NULL, 0);
    }
    check(name, WIFEXITED(st) && WEXITSTATUS(st) == 0);
}

static void peer_cred(void) {
    struct sockaddr_un a = {.sun_family = AF_UNIX};
    memcpy(a.sun_path, "\0credtest", 9);
    socklen_t len = offsetof(struct sockaddr_un, sun_path) + 9;
    int l = socket(AF_UNIX, SOCK_STREAM, 0);
    int ok = l >= 0 && bind(l, (void *)&a, len) == 0 && listen(l, 1) == 0;
    pid_t pid = fork();
    if (pid == 0) {
        int c = socket(AF_UNIX, SOCK_STREAM, 0);
        _exit(setegid(55) || seteuid(1234) || connect(c, (void *)&a, len) || write(c, "x", 1) != 1);
    }
    int s = accept(l, NULL, NULL);
    char x;
    struct ucred uc;
    socklen_t ul = sizeof uc;
    ok = ok && s >= 0 && read(s, &x, 1) == 1 &&
         getsockopt(s, SOL_SOCKET, SO_PEERCRED, &uc, &ul) == 0 && uc.pid == pid && uc.uid == 1234 && uc.gid == 55;
    waitpid(pid, NULL, 0);
    close(s);
    close(l);
    check("peercred", ok);
}

/* probe is the set-id copy's side: who it runs as, and AT_SECURE */
static int probe(void) {
    return !(resuid(2000, 1000, 1000) && resgid(2000, 1000, 1000) && getauxval(AT_SECURE) == 1);
}

static void setid_exec(const char *self) {
    struct stat sb; /* /tmp is often nosuid on Linux (WSL); Burrow has no /var */
    const char *copy = stat("/var/tmp", &sb) == 0 ? "/var/tmp/credtest-setid" : "/tmp/credtest-setid";
    char buf[65536];
    FILE *in = fopen(self, "rb"), *out = fopen(copy, "wb");
    size_t n;
    while (in && out && (n = fread(buf, 1, sizeof buf, in)) > 0)
        fwrite(buf, 1, n, out);
    int ok = in && out && fclose(out) == 0 && chown(copy, 1000, 1000) == 0 && chmod(copy, 06755) == 0;
    if (in)
        fclose(in);
    pid_t pid = fork();
    if (pid == 0) {
        if (setgid(2000) == 0 && setuid(2000) == 0)
            execl(copy, "credtest", "--probe", (char *)NULL);
        _exit(2);
    }
    int st;
    waitpid(pid, &st, 0);
    unlink(copy);
    check("setid-exec", ok && WIFEXITED(st) && WEXITSTATUS(st) == 0);
}

int main(int argc, char **argv) {
    if (argc > 1 && strcmp(argv[1], "--probe") == 0)
        return probe();
    setvbuf(stdout, NULL, _IONBF, 0);
    check("root", resuid(0, 0, 0) && resgid(0, 0, 0) && getauxval(AT_SECURE) == 0);
    groups();
    in_child("drop-for-good", drop_for_good);
    in_child("temporary-drop", temporary_drop);
    in_child("setresuid", set_res);
    in_child("setreuid", set_re);
    in_child("fsuid", fs_ids);
    in_child("kill", kill_rules);
    peer_cred();
    if (argc > 1 && strcmp(argv[1], "--exec") == 0)
        setid_exec(argc > 2 ? argv[2] : "/proc/self/exe");
    printf("credtest: %s\n", failures ? "FAIL" : "ok");
    return failures != 0;
}
