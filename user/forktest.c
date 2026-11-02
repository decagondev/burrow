/* forktest.c - fork, execve and waitpid end to end (W10-S02): one child
 * exits with a status, one becomes /bin/hello, one is killed by a fault. */
#include <stdio.h>
#include <stdlib.h>
#include <sys/wait.h>
#include <unistd.h>

static void report(const char *what, pid_t pid) {
    int status;
    if (waitpid(pid, &status, 0) != pid) {
        printf("forktest: waitpid failed for %s\n", what);
        exit(1);
    }
    if (WIFEXITED(status))
        printf("forktest: %s exited with %d\n", what, WEXITSTATUS(status));
    else if (WIFSIGNALED(status))
        printf("forktest: %s killed by signal %d\n", what, WTERMSIG(status));
}

int main(void) {
    int shared = 1;
    pid_t pid = fork();
    if (pid == 0) {
        shared = 2;                        /* the child's own copy */
        exit(7);
    }
    report("child", pid);
    printf("forktest: parent still sees %d\n", shared);

    fflush(stdout);
    pid = fork();
    if (pid == 0) {
        char *argv[] = { "/bin/hello", 0 };
        execv(argv[0], argv);
        _exit(127);
    }
    report("exec child", pid);

    pid = fork();
    if (pid == 0) {
        volatile int *null = 0;
        return *null;
    }
    report("crashing child", pid);
    printf("forktest: my parent is %d\n", (int)getppid());
    return 0;
}
