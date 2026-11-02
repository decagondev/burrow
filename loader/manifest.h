/* manifest.h (loader side) - validate and query the on-disk manifest. Pure logic. */
#ifndef LOADER_MANIFEST_H
#define LOADER_MANIFEST_H
#include <burrow/manifest.h>

enum manifest_status {
    MANIFEST_OK = 0,
    MANIFEST_BAD_MAGIC,
    MANIFEST_BAD_VERSION,
    MANIFEST_BAD_COUNT,
    MANIFEST_BAD_ENTRY,      /* sectors too small for size_bytes, or lba < first payload */
};

enum manifest_status manifest_validate(const struct manifest *m);
const struct manifest_entry *manifest_find(const struct manifest *m, const char *name);
const char *manifest_status_str(enum manifest_status s);
#endif
