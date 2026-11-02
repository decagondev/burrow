/* lib_string.h - freestanding string/memory helpers.
 * GCC may emit calls to memcpy/memset/memcmp even with -ffreestanding,
 * so these MUST exist with exactly these names. */
#ifndef COMMON_LIB_STRING_H
#define COMMON_LIB_STRING_H
#include <stddef.h>

void  *memset(void *dst, int c, size_t n);
void  *memcpy(void *dst, const void *src, size_t n);
int    memcmp(const void *a, const void *b, size_t n);
size_t strlen(const char *s);
int    strncmp(const char *a, const char *b, size_t n);
#endif
