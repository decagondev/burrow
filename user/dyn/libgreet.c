/* libgreet.c - a shared library dynhello links against (W18-S05): its
 * code and its data live once in /lib/libgreet.so and are mapped by musl's
 * loader. Never used by anything else. Docs: WAVE-18-foundations.md. */
int greet_calls; /* data the library owns: a relocation the loader fills */

const char *greet(void) {
    greet_calls++;
    return "hello from a shared library";
}
