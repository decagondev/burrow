/* minitest.h - a unit test "framework" small enough to read in one sitting.
 *
 *   TEST(name) { CHECK(cond); CHECK_EQ_U64(a, b); CHECK_STR(a, b); }
 *   In test_main.c:  RUN(name);
 *
 * No auto-registration magic: test_main.c lists every test explicitly, so a
 * beginner can see exactly what runs. */
#ifndef MINITEST_H
#define MINITEST_H
#include <stdio.h>
#include <string.h>

extern int mt_failures, mt_checks;

#define TEST(name) void test_##name(void)
#define RUN(name) do { int before = mt_failures; test_##name(); \
    printf("%s %s\n", mt_failures == before ? "  ok  " : "  FAIL", #name); } while (0)

#define CHECK(cond) do { mt_checks++; if (!(cond)) { mt_failures++; \
    printf("    %s:%d: CHECK(%s)\n", __FILE__, __LINE__, #cond); } } while (0)

#define CHECK_EQ_U64(a, b) do { unsigned long long _a = (a), _b = (b); mt_checks++; \
    if (_a != _b) { mt_failures++; printf("    %s:%d: %s == 0x%llx, want 0x%llx\n", \
    __FILE__, __LINE__, #a, _a, _b); } } while (0)

#define CHECK_STR(a, b) do { const char *_a = (a), *_b = (b); mt_checks++; \
    if (strcmp(_a, _b) != 0) { mt_failures++; printf("    %s:%d: got \"%s\", want \"%s\"\n", \
    __FILE__, __LINE__, _a, _b); } } while (0)
#endif
