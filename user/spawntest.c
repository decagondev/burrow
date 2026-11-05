/* spawntest.c - system() and posix_spawn() (BUG-0028, W22-S03): musl runs
 * both through clone(CLONE_VM|CLONE_VFORK) onto a stack holding the
 * function the child runs; Burrow's fork shape used to ignore that stack
 * and the child jumped to garbage. Each child runs /bin/echo, and the
 * parent checks its exit status. Prints "spawntest: ok" when every check
 * holds; passes on the host's Linux too. Docs: BUGS_AND_MITIGATIONS.md. */
#include <spawn.h>
#include <stdio.h>
#include <stdlib.h>
#include <sys/wait.h>

extern char **environ;

int main(void) {
    fflush(stdout);
    int st = system("/bin/echo spawntest: system ran echo");
    if (st == -1 || !WIFEXITED(st) || WEXITSTATUS(st) != 0) {
        printf("spawntest: FAIL system() = %#x\n", st);
        return 1;
    }
    pid_t pid;
    char *argv[] = {"echo", "spawntest: posix_spawn ran echo", NULL};
    fflush(stdout);
    int e = posix_spawn(&pid, "/bin/echo", NULL, NULL, argv, environ);
    if (e != 0 || waitpid(pid, &st, 0) != pid || !WIFEXITED(st) || WEXITSTATUS(st) != 0) {
        printf("spawntest: FAIL posix_spawn = %d, status %#x\n", e, st);
        return 1;
    }
    char *missing[] = {"nothing", NULL};
    e = posix_spawn(&pid, "/bin/no-such-program", NULL, NULL, missing, environ);
    if (e == 0) {
        printf("spawntest: FAIL posix_spawn of a missing program succeeded\n");
        return 1;
    }
    puts("spawntest: ok");
    return 0;
}
