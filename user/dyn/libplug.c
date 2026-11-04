/* libplug.c - a plug-in dynhello opens at run time with dlopen (W18-S05).
 * Never linked against anything at build time. Docs: WAVE-18-foundations.md. */
int plug_answer(int x) { return x * 6; }
