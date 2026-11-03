/* ls.c - list directories, one name per line (-l: type, size, name). */
#include <dirent.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <sys/stat.h>

static int cmp(const void *a, const void *b) { return strcmp(*(char *const *)a, *(char *const *)b); }

static int list(const char *dir, int lng) {
    DIR *d = opendir(dir);
    if (!d) { perror(dir); return 1; }
    char *names[512];
    int n = 0;
    for (struct dirent *e; (e = readdir(d)) && n < 512;)
        if (e->d_name[0] != '.') names[n++] = strdup(e->d_name);
    closedir(d);
    qsort(names, n, sizeof names[0], cmp);
    for (int i = 0; i < n; i++) {
        if (lng) {
            char path[512];
            struct stat st;
            snprintf(path, sizeof path, "%s/%s", dir, names[i]);
            if (stat(path, &st) == 0)
                printf("%c %8ld %s\n", S_ISDIR(st.st_mode) ? 'd' : S_ISCHR(st.st_mode) ? 'c' : '-',
                       (long)st.st_size, names[i]);
        } else {
            puts(names[i]);
        }
        free(names[i]);
    }
    return 0;
}

int main(int argc, char **argv) {
    int lng = argc > 1 && !strcmp(argv[1], "-l"), rc = 0;
    int first = 1 + lng;
    if (first >= argc) return list(".", lng);
    for (int i = first; i < argc; i++) rc |= list(argv[i], lng);
    return rc;
}
