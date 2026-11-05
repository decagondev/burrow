/* sh.c - Burrow's shell (W10-S06): enough of a POSIX shell to use the
 * system and prove the kernel. Lines are split into words ('...' and "..."
 * quote, $? is the last status); `;` separates commands; `|` builds
 * pipelines; `<`, `>` and `>>` redirect. Builtins: cd, pwd, exit. Every
 * job runs in its own process group and owns the terminal while it runs,
 * so ^C stops the job and never the shell. `sh -c LINE` (BUG-0028: what
 * system() and popen() run) runs one line without job control, never
 * touching the terminal's foreground group, and exits with its status.
 * Not a full sh: no variables, no globbing, no control flow, no background
 * jobs. */
#include <errno.h>
#include <fcntl.h>
#include <signal.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <sys/wait.h>
#include <unistd.h>

#define MAXWORDS 64
#define MAXCMDS  8

struct cmd {
    char *argv[MAXWORDS];
    char *in, *out;
    int append;
};

static int last_status;
static char status_text[12];

static void on_sigint(int sig) { (void)sig; }   /* the prompt survives ^C; exec resets it */

/* Splits line into words in place; returns the count. Quotes join, `|`,
 * `;`, `<` and `>` stand alone. */
static int split(char *line, char **w) {
    int n = 0;
    static char buf[4096];
    char *o = buf;
    for (char *p = line; *p && n < MAXWORDS - 1;) {
        while (*p == ' ' || *p == '\t' || *p == '\n') p++;
        if (!*p) break;
        w[n++] = o;
        if (strchr("|;<", *p)) { *o++ = *p++; *o++ = 0; continue; }
        if (*p == '>') { *o++ = *p++; if (*p == '>') *o++ = *p++; *o++ = 0; continue; }
        while (*p && !strchr(" \t\n|;<>", *p)) {
            if (*p == '\'' || *p == '"') {
                char q = *p++;
                while (*p && *p != q) *o++ = *p++;
                if (*p) p++;
            } else if (p[0] == '$' && p[1] == '?') {
                for (char *s = status_text; *s;) *o++ = *s++;
                p += 2;
            } else {
                *o++ = *p++;
            }
        }
        *o++ = 0;
    }
    w[n] = 0;
    return n;
}

static int interactive = 1;   /* 0 for sh -c: no process groups, no terminal */

static void run_child(struct cmd *c) {
    if (c->in) {
        int fd = open(c->in, O_RDONLY);
        if (fd < 0) { perror(c->in); _exit(1); }
        dup2(fd, 0);
        close(fd);
    }
    if (c->out) {
        int fd = open(c->out, O_WRONLY | O_CREAT | (c->append ? O_APPEND : O_TRUNC), 0644);
        if (fd < 0) { perror(c->out); _exit(1); }
        dup2(fd, 1);
        close(fd);
    }
    char path[256];
    const char *prog = c->argv[0];
    if (!strchr(prog, '/')) {
        snprintf(path, sizeof path, "/bin/%s", prog);
        prog = path;
    }
    signal(SIGINT, SIG_DFL);
    signal(SIGQUIT, SIG_DFL);
    execv(prog, c->argv);
    fprintf(stderr, "sh: %s: %s\n", c->argv[0], errno == ENOENT ? "not found" : strerror(errno));
    _exit(127);
}

static int builtin(struct cmd *c) {
    const char *name = c->argv[0];
    if (!strcmp(name, "exit")) exit(c->argv[1] ? atoi(c->argv[1]) : last_status);
    if (!strcmp(name, "cd")) {
        if (chdir(c->argv[1] ? c->argv[1] : "/") != 0) { perror("cd"); last_status = 1; }
        else last_status = 0;
        return 1;
    }
    if (!strcmp(name, "pwd")) {
        char buf[256];
        puts(getcwd(buf, sizeof buf) ? buf : "?");
        last_status = 0;
        return 1;
    }
    return 0;
}

/* Runs one pipeline of n commands as a job in its own process group. */
static void run_pipeline(struct cmd *cmds, int n) {
    if (n == 1 && builtin(&cmds[0])) return;
    pid_t pids[MAXCMDS], pgid = 0;
    int prev = -1;
    for (int i = 0; i < n; i++) {
        int fds[2] = { -1, -1 };
        if (i < n - 1 && pipe(fds) != 0) { perror("pipe"); return; }
        pid_t pid = fork();
        if (pid == 0) {
            if (interactive) setpgid(0, pgid);
            if (prev >= 0) { dup2(prev, 0); close(prev); }
            if (fds[1] >= 0) { dup2(fds[1], 1); close(fds[1]); close(fds[0]); }
            run_child(&cmds[i]);
        }
        if (!pgid) pgid = pid;
        if (interactive) setpgid(pid, pgid);   /* both sides, so no race decides it */
        pids[i] = pid;
        if (prev >= 0) close(prev);
        if (fds[1] >= 0) close(fds[1]);
        prev = fds[0];
    }
    if (interactive) tcsetpgrp(0, pgid);   /* the terminal is the job's: ^C goes there */
    int status = 0;
    for (int i = 0; i < n; i++) waitpid(pids[i], &status, 0);
    if (interactive) tcsetpgrp(0, getpgrp());
    if (WIFSIGNALED(status)) {
        last_status = 128 + WTERMSIG(status);
        if (WTERMSIG(status) != SIGINT) printf("sh: killed by signal %d\n", WTERMSIG(status));
    } else {
        last_status = WEXITSTATUS(status);
    }
}

/* Parses words into pipelines separated by `;` and runs them. */
static void run_line(char **w, int n) {
    struct cmd cmds[MAXCMDS];
    int nc = 0, na = 0;
    memset(cmds, 0, sizeof cmds);
    for (int i = 0; i <= n; i++) {
        char *t = i < n ? w[i] : ";";
        if (!strcmp(t, "|") || !strcmp(t, ";")) {
            cmds[nc].argv[na] = 0;
            if (na) nc++;
            na = 0;
            if (!strcmp(t, ";") && nc) {
                run_pipeline(cmds, nc);
                snprintf(status_text, sizeof status_text, "%d", last_status);
                memset(cmds, 0, sizeof cmds);
                nc = 0;
            }
            if (nc == MAXCMDS) { fprintf(stderr, "sh: pipeline too long\n"); return; }
        } else if ((!strcmp(t, "<") || !strcmp(t, ">") || !strcmp(t, ">>")) && i + 1 < n) {
            if (t[0] == '<') cmds[nc].in = w[++i];
            else { cmds[nc].out = w[++i]; cmds[nc].append = t[1] == '>'; }
        } else if (na < MAXWORDS - 1) {
            cmds[nc].argv[na++] = t;
        }
    }
}

int main(int argc, char **argv) {
    char line[1024], *w[MAXWORDS];
    snprintf(status_text, sizeof status_text, "0");
    if (argc >= 3 && !strcmp(argv[1], "-c")) {   /* system(), popen() */
        interactive = 0;
        snprintf(line, sizeof line, "%s", argv[2]);
        int n = split(line, w);
        if (n) run_line(w, n);
        return last_status;
    }
    struct sigaction sa;
    memset(&sa, 0, sizeof sa);
    sa.sa_handler = on_sigint;
    sigaction(SIGINT, &sa, 0);
    sigaction(SIGQUIT, &sa, 0);
    setpgid(0, 0);
    tcsetpgrp(0, getpgrp());
    for (;;) {
        fputs("$ ", stdout);
        fflush(stdout);
        if (!fgets(line, sizeof line, stdin)) {
            if (errno == EINTR) { clearerr(stdin); errno = 0; putchar('\n'); continue; }
            break;                   /* ^D: end of input */
        }
        int n = split(line, w);
        if (n) run_line(w, n);
    }
    putchar('\n');
    return last_status;
}
