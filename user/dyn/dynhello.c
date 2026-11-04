/* dynhello.c - a dynamically linked program (W18-S05): started through
 * musl's loader (PT_INTERP /lib/ld-musl-x86_64.so.1), it calls into
 * libgreet.so, linked at build time, and into libplug.so, opened with
 * dlopen and dlsym at run time. Runs on the host's Linux (with
 * LD_LIBRARY_PATH at the build's libraries) and on Burrow (libraries in
 * /lib). Prints "dynhello: ..." lines and "dynhello: ok".
 * Docs: docs/04-plan/waves/WAVE-18-foundations.md. */
#include <dlfcn.h>
#include <stdio.h>
#include <string.h>

const char *greet(void);
extern int greet_calls;

int main(void) {
    int failures = 0;
    const char *g = greet();
    int linked = strcmp(g, "hello from a shared library") == 0 && greet_calls == 1;
    printf("dynhello: libgreet says %s %s\n", g, linked ? "ok" : "FAIL");
    failures += !linked;
    void *h = dlopen("libplug.so", RTLD_NOW);
    int (*answer)(int) = h ? (int (*)(int))dlsym(h, "plug_answer") : NULL;
    int plugged = answer && answer(7) == 42;
    printf("dynhello: dlopen libplug.so, plug_answer(7) = %d %s\n", answer ? answer(7) : -1, plugged ? "ok" : "FAIL");
    if (!h)
        printf("dynhello: dlopen: %s\n", dlerror());
    failures += !plugged;
    printf("dynhello: %s\n", failures ? "FAIL" : "ok");
    return failures != 0;
}
