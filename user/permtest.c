/* permtest.c - owners and permissions (W19-S04): who may read, write,
 * create, delete, search and run what, the sticky and set-group-id bits on
 * directories, chmod and chown rules, access(2) with the real ids, the
 * umask, and set-id bits dropped by chown and by a user's write. Works in a
 * fresh directory (argv[1], default /tmp/permtest) that it removes after.
 * Each user-side case runs in a child that becomes user 1000 (or 1001).
 * Prints "permtest: <case> ok|FAIL", then "permtest: ok" or "FAIL".
 * Must run as root. Docs: docs/04-plan/waves/WAVE-19-unix.md (S04). */
#define _GNU_SOURCE
#include <dirent.h>
#include <errno.h>
#include <fcntl.h>
#include <grp.h>
#include <stdio.h>
#include <string.h>
#include <sys/stat.h>
#include <sys/wait.h>
#include <unistd.h>

static const char *D = "/tmp/permtest";
static int failures;

static const char *at(const char *sub) {
    static char buf[4][512];
    static int i;
    i = (i + 1) % 4;
    snprintf(buf[i], sizeof buf[i], "%s/%s", D, sub);
    return buf[i];
}

static void check(const char *name, int ok) {
    printf("permtest: %s %s\n", name, ok ? "ok" : "FAIL");
    failures += !ok;
}

static int make(const char *sub, int mode, uid_t uid, gid_t gid) { /* as root */
    int fd = open(at(sub), O_WRONLY | O_CREAT | O_TRUNC, mode);
    int ok = fd >= 0 && write(fd, "data", 4) == 4 && close(fd) == 0 && chown(at(sub), uid, gid) == 0;
    return ok && chmod(at(sub), mode) == 0;
}

static int dir(const char *sub, int mode, uid_t uid, gid_t gid) {
    return mkdir(at(sub), mode) == 0 && chown(at(sub), uid, gid) == 0 && chmod(at(sub), mode) == 0;
}

static int err(int r, int e) { return r == -1 && errno == e; }
static int opens(const char *sub, int flags) { int fd = open(at(sub), flags, 0644); return fd >= 0 && close(fd) == 0; }
static int mode_of(const char *sub) { struct stat st; return stat(at(sub), &st) ? -1 : (int)(st.st_mode & 07777); }

static int owner_is(const char *sub, uid_t u, gid_t g) {
    struct stat st;
    return stat(at(sub), &st) == 0 && st.st_uid == u && st.st_gid == g;
}

#define NEED(x) do { if (!(x)) return __LINE__; } while (0)

static void as_user(const char *name, uid_t uid, int (*fn)(void)) {
    pid_t pid = fork();
    if (pid == 0) {
        if (setgroups(0, NULL) || setgid(uid) || setuid(uid))
            _exit(99);
        umask(022);
        int line = fn();
        if (line)
            printf("permtest: %s went wrong at line %d\n", name, line);
        _exit(line != 0);
    }
    int st;
    waitpid(pid, &st, 0);
    check(name, WIFEXITED(st) && WEXITSTATUS(st) == 0);
}

static int owner(void) {
    int fd = open(at("mine"), O_WRONLY | O_CREAT, 0666);
    NEED(fd >= 0 && write(fd, "x", 1) == 1 && close(fd) == 0);
    NEED(owner_is("mine", 1000, 1000) && mode_of("mine") == 0644); /* the umask applies */
    NEED(mkdir(at("mydir"), 0777) == 0 && mode_of("mydir") == 0755);
    return 0;
}

static int denied(void) {
    NEED(err(open(at("secret"), O_RDONLY), EACCES) && err(open(at("secret"), O_WRONLY), EACCES));
    NEED(err(open(at("secret"), O_WRONLY | O_CREAT, 0644), EACCES));
    NEED(opens("grp", O_RDONLY) && err(open(at("grp"), O_WRONLY), EACCES)); /* group read only */
    NEED(opens("oth", O_RDONLY) && err(open(at("oth"), O_RDWR), EACCES));  /* others read only */
    return 0;
}

static int no_write_dir(void) {
    NEED(err(open(at("ro/new"), O_WRONLY | O_CREAT, 0644), EACCES) && err(mkdir(at("ro/d"), 0755), EACCES));
    NEED(err(unlink(at("ro/f")), EACCES) && err(rename(at("ro/f"), at("moved")), EACCES));
    NEED(opens("ro/f", O_RDONLY)); /* reading is fine */
    return 0;
}

static int no_search(void) {
    struct stat st;
    NEED(err(stat(at("nox/f"), &st), EACCES) && err(open(at("nox/f"), O_RDONLY), EACCES));
    NEED(err(chdir(at("nox")), EACCES));
    return 0;
}

static int sticky_1000(void) {
    NEED(err(unlink(at("st/rootf")), EPERM) && err(rename(at("st/rootf"), at("st/x")), EPERM));
    NEED(opens("st/mine", O_WRONLY | O_CREAT) && opens("st/keep", O_WRONLY | O_CREAT));
    NEED(unlink(at("st/mine")) == 0);
    return 0;
}

static int sticky_1001(void) {
    NEED(err(unlink(at("st/keep")), EPERM)); /* 1000's file, 1001 in a sticky directory */
    return 0;
}

static int setgid_dir(void) {
    NEED(opens("sg/f", O_WRONLY | O_CREAT) && owner_is("sg/f", 1000, 50));
    NEED(mkdir(at("sg/sub"), 0755) == 0 && owner_is("sg/sub", 1000, 50) && (mode_of("sg/sub") & 02000));
    return 0;
}

static int chmod_chown(void) {
    NEED(err(chmod(at("secret"), 0644), EPERM) && chmod(at("mine"), 0600) == 0 && mode_of("mine") == 0600);
    NEED(err(chown(at("mine"), 1001, -1), EPERM) && err(chown(at("mine"), -1, 50), EPERM));
    NEED(chown(at("mine"), -1, 1000) == 0);
    NEED(chmod(at("theirgrp"), 02755) == 0 && mode_of("theirgrp") == 0755); /* group 50 is not ours */
    NEED(chmod(at("mine"), 02755) == 0 && mode_of("mine") == 02755);       /* group 1000 is */
    int fd = open(at("sw"), O_WRONLY);
    NEED(fd >= 0 && write(fd, "y", 1) == 1 && close(fd) == 0 && mode_of("sw") == 0777); /* a write drops set-uid */
    return 0;
}

static int run_it(void) {
    char *argv[] = {"x", NULL};
    NEED(err(execv(at("rootx"), argv), EACCES));
    return 0;
}

static void rmtree(const char *p) {
    DIR *d = opendir(p);
    struct dirent *e;
    while (d && (e = readdir(d))) {
        char q[512];
        if (!strcmp(e->d_name, ".") || !strcmp(e->d_name, ".."))
            continue;
        snprintf(q, sizeof q, "%s/%s", p, e->d_name);
        struct stat st;
        if (lstat(q, &st) == 0 && S_ISDIR(st.st_mode))
            rmtree(q);
        else
            unlink(q);
    }
    if (d)
        closedir(d);
    rmdir(p);
}

static void as_root(void) {
    char *argv[] = {"x", NULL};
    check("root-reads-anything", make("zero", 0, 0, 0) && opens("zero", O_RDWR));
    check("root-needs-an-x-bit", make("noexec", 0644, 0, 0) && err(execv(at("noexec"), argv), EACCES));
    pid_t pid = fork();
    if (pid == 0) /* real 1000, effective root: open works, access says no */
        _exit(!(setresuid(1000, 0, 0) == 0 && err(access(at("secret"), R_OK), EACCES) &&
                opens("secret", O_RDONLY) && err(access(at("oth"), R_OK), EACCES))); /* group 0 is ours: its bits, not the others', count */
    int st;
    waitpid(pid, &st, 0);
    check("access-uses-real-ids", WIFEXITED(st) && WEXITSTATUS(st) == 0);
    umask(077);
    int fd = open(at("um"), O_WRONLY | O_CREAT, 0666);
    check("umask", fd >= 0 && close(fd) == 0 && mode_of("um") == 0600 && mkdir(at("umd"), 0777) == 0 && mode_of("umd") == 0700);
    umask(0);
    check("chown-drops-set-uid", make("su", 04755, 1000, 1000) && chown(at("su"), 1001, -1) == 0 && mode_of("su") == 0755);
    check("root-chowns", chown(at("mine"), 1001, 1001) == 0 && owner_is("mine", 1001, 1001));
    check("sticky-root-deletes", unlink(at("st/keep")) == 0);
}

int main(int argc, char **argv) {
    setvbuf(stdout, NULL, _IONBF, 0);
    if (argc > 1)
        D = argv[1];
    umask(0);
    rmtree(D);
    int ok = mkdir(D, 0777) == 0 && chmod(D, 0777) == 0 && make("secret", 0600, 0, 0) && make("grp", 0640, 0, 1000) &&
             make("oth", 0604, 0, 0) && dir("ro", 0755, 0, 0) && make("ro/f", 0666, 0, 0) && dir("nox", 0700, 0, 0) &&
             make("nox/f", 0644, 0, 0) && dir("st", 01777, 0, 0) && make("st/rootf", 0666, 0, 0) &&
             dir("sg", 02777, 0, 50) && make("rootx", 0700, 0, 0) && make("sw", 04777, 1000, 1000);
    check("setup", ok);
    as_user("owner", 1000, owner);
    ok = make("theirgrp", 0755, 1000, 50);
    as_user("denied", 1000, denied);
    as_user("no-write-dir", 1000, no_write_dir);
    as_user("no-search", 1000, no_search);
    as_user("sticky", 1000, sticky_1000);
    as_user("sticky-other-user", 1001, sticky_1001);
    as_user("setgid-dir", 1000, setgid_dir);
    as_user("chmod-chown", 1000, chmod_chown);
    as_user("exec", 1000, run_it);
    as_root();
    rmtree(D);
    printf("permtest: %s\n", failures || !ok ? "FAIL" : "ok");
    return failures != 0;
}
