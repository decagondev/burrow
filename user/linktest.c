/* linktest.c - symbolic and hard links (W19-S05): symlink, readlink, lstat
 * against stat, following and O_NOFOLLOW, dangling links and creating
 * through them, loops (ELOOP), ".." after a link (taken from where the
 * link led, not from the text), absolute links, unlink and rename of a
 * link, hard links (one inode, a link count, data shared), link of a
 * directory, chdir through a link and getcwd, the *at forms, and a
 * trailing slash on a file. Works in a fresh directory (argv[1], default
 * /tmp/linktest) that it removes after. Prints "linktest: <case> ok|FAIL",
 * then "linktest: ok" or "FAIL". Docs: docs/04-plan/waves/WAVE-19-unix.md. */
#define _GNU_SOURCE
#include <dirent.h>
#include <errno.h>
#include <fcntl.h>
#include <stdio.h>
#include <string.h>
#include <sys/stat.h>
#include <unistd.h>

static const char *D = "/tmp/linktest";
static int failures;

static const char *at(const char *sub) {
    static char buf[4][512];
    static int i;
    i = (i + 1) % 4;
    snprintf(buf[i], sizeof buf[i], "%s/%s", D, sub);
    return buf[i];
}

static void check(const char *name, int ok) {
    printf("linktest: %s %s\n", name, ok ? "ok" : "FAIL");
    failures += !ok;
}

static int err(int r, int e) { return r == -1 && errno == e; }

static int put(const char *sub, const char *data) {
    int fd = open(at(sub), O_WRONLY | O_CREAT | O_TRUNC, 0644);
    return fd >= 0 && write(fd, data, strlen(data)) == (ssize_t)strlen(data) && close(fd) == 0;
}

static int reads(const char *path, const char *want) {
    char b[64] = {0};
    int fd = open(path, O_RDONLY);
    return fd >= 0 && read(fd, b, sizeof b - 1) == (ssize_t)strlen(want) && close(fd) == 0 && !strcmp(b, want);
}

static int link_is(const char *sub, const char *want) {
    char b[256];
    ssize_t n = readlink(at(sub), b, sizeof b);
    return n == (ssize_t)strlen(want) && !memcmp(b, want, n);
}

static void rmtree(const char *p) {
    DIR *d = opendir(p);
    struct dirent *e;
    while (d && (e = readdir(d))) {
        char q[512];
        struct stat st;
        if (!strcmp(e->d_name, ".") || !strcmp(e->d_name, ".."))
            continue;
        snprintf(q, sizeof q, "%s/%s", p, e->d_name);
        if (lstat(q, &st) == 0 && S_ISDIR(st.st_mode))
            rmtree(q);
        else
            unlink(q);
    }
    if (d)
        closedir(d);
    rmdir(p);
}

static void basics(void) {
    char small[4];
    check("symlink-readlink", symlink("target-text", at("l")) == 0 && link_is("l", "target-text") &&
                                  readlink(at("l"), small, sizeof small) == 4 && !memcmp(small, "targ", 4));
    check("readlink-of-a-file", put("f", "contents of f") && err(readlink(at("f"), small, 4), EINVAL));
    struct stat ls, st;
    check("lstat-and-stat", symlink("f", at("lf")) == 0 && lstat(at("lf"), &ls) == 0 && S_ISLNK(ls.st_mode) &&
                                ls.st_size == 1 && stat(at("lf"), &st) == 0 && S_ISREG(st.st_mode) && st.st_size == 13);
    check("follow", reads(at("lf"), "contents of f"));
    check("nofollow", err(open(at("lf"), O_RDONLY | O_NOFOLLOW), ELOOP));
    check("fstatat-nofollow", fstatat(AT_FDCWD, at("lf"), &ls, AT_SYMLINK_NOFOLLOW) == 0 && S_ISLNK(ls.st_mode));
}

static void dangling_and_loops(void) {
    struct stat st;
    int ok = symlink("nowhere", at("dang")) == 0 && err(stat(at("dang"), &st), ENOENT) && lstat(at("dang"), &st) == 0;
    int fd = open(at("dang"), O_WRONLY | O_CREAT, 0644); /* creates what it points at */
    check("dangling", ok && fd >= 0 && close(fd) == 0 && stat(at("nowhere"), &st) == 0);
    check("excl-on-a-link", symlink("nowhere2", at("dang2")) == 0 &&
                                err(open(at("dang2"), O_WRONLY | O_CREAT | O_EXCL, 0644), EEXIST));
    check("mkdir-over-a-link", err(mkdir(at("dang2"), 0755), EEXIST));
    check("loop", symlink("loop2", at("loop1")) == 0 && symlink("loop1", at("loop2")) == 0 &&
                      err(open(at("loop1"), O_RDONLY), ELOOP) && err(stat(at("loop1"), &st), ELOOP));
}

static void directories(void) {
    char cwd[512], want[512];
    int ok = mkdir(at("a"), 0755) == 0 && mkdir(at("a/b"), 0755) == 0 && put("a/x", "x in a") && symlink("a/b", at("ab")) == 0;
    check("dotdot-after-a-link", ok && reads(at("ab/../x"), "x in a")); /* the text says D/x, which is not there */
    char abs[512];
    snprintf(abs, sizeof abs, "%s/f", D);
    check("absolute-link", symlink(abs, at("abs")) == 0 && reads(at("abs"), "contents of f"));
    snprintf(want, sizeof want, "%s/a/b", D);
    int back = open(".", O_RDONLY | O_DIRECTORY);
    check("chdir-through-a-link", chdir(at("ab")) == 0 && getcwd(cwd, sizeof cwd) && !strcmp(cwd, want));
    if (back >= 0) {
        fchdir(back);
        close(back);
    }
    check("trailing-slash-on-a-file", err(open(at("f/"), O_RDONLY), ENOTDIR));
}

static void names(void) {
    struct stat st;
    check("unlink-a-link", unlink(at("lf")) == 0 && err(lstat(at("lf"), &st), ENOENT) && reads(at("f"), "contents of f"));
    check("rename-a-link", rename(at("l"), at("l2")) == 0 && link_is("l2", "target-text"));
    int dfd = open(D, O_RDONLY | O_DIRECTORY);
    char b[16];
    ssize_t n = dfd >= 0 ? readlinkat(dfd, "rel", b, sizeof b) : -1;
    check("at-forms", dfd >= 0 && symlinkat("f", dfd, "rel") == 0 && (n = readlinkat(dfd, "rel", b, sizeof b)) == 1 &&
                          b[0] == 'f' && linkat(dfd, "f", dfd, "hard-at", 0) == 0);
    if (dfd >= 0)
        close(dfd);
}

static void hard(void) {
    struct stat a, b;
    int ok = link(at("f"), at("h")) == 0 && stat(at("f"), &a) == 0 && stat(at("h"), &b) == 0;
    check("hard-link", ok && a.st_ino == b.st_ino && a.st_nlink == 3 && b.st_nlink == 3); /* f, hard-at, h */
    check("hard-link-shares-data", put("h", "new data") && reads(at("f"), "new data"));
    check("hard-link-outlives-a-name", unlink(at("f")) == 0 && reads(at("h"), "new data") && stat(at("h"), &b) == 0 && b.st_nlink == 2);
    check("link-of-a-directory", err(link(at("a"), at("a2")), EPERM));
    check("link-to-an-existing-name", err(link(at("h"), at("hard-at")), EEXIST));
}

int main(int argc, char **argv) {
    setvbuf(stdout, NULL, _IONBF, 0);
    if (argc > 1)
        D = argv[1];
    rmtree(D);
    check("setup", mkdir(D, 0755) == 0);
    basics();
    dangling_and_loops();
    directories();
    names();
    hard();
    rmtree(D);
    struct stat st;
    check("cleanup", err(lstat(D, &st), ENOENT));
    printf("linktest: %s\n", failures ? "FAIL" : "ok");
    return failures != 0;
}
