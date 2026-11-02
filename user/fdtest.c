/* fdtest.c - descriptors and paths end to end (W10-S03): a pipe carries an
 * exec'd program's stdout, a file is written and read back under /tmp, a
 * directory is listed, and the working directory moves. */
#include <dirent.h>
#include <stdio.h>
#include <string.h>
#include <sys/wait.h>
#include <unistd.h>

int main(void) {
    int fds[2];
    if (pipe(fds) != 0) return 1;
    pid_t pid = fork();
    if (pid == 0) {
        dup2(fds[1], 1);
        close(fds[0]);
        close(fds[1]);
        execl("/bin/hello", "hello", (char *)0);
        _exit(127);
    }
    close(fds[1]);
    char buf[128];
    ssize_t n, total = 0;
    while ((n = read(fds[0], buf + total, sizeof buf - 1 - total)) > 0) total += n;
    buf[total] = 0;
    waitpid(pid, 0, 0);
    printf("fdtest: the pipe carried %d bytes: %s", (int)total, buf);

    FILE *f = fopen("/tmp/note", "w");
    if (!f) return 2;
    fprintf(f, "written by fdtest\n");
    fclose(f);
    f = fopen("/tmp/note", "r");
    if (!f || !fgets(buf, sizeof buf, f)) return 3;
    fclose(f);
    printf("fdtest: /tmp/note says: %s", buf);

    DIR *d = opendir("/");
    if (!d) return 4;
    printf("fdtest: / holds:");
    for (struct dirent *e; (e = readdir(d));)
        if (e->d_name[0] != '.') printf(" %s", e->d_name);
    printf("\n");
    closedir(d);

    if (chdir("/etc") != 0 || !getcwd(buf, sizeof buf)) return 5;
    printf("fdtest: cwd is now %s\n", buf);
    return 0;
}
