#include "manifest.h"
#include "lib_string.h"

#define FIRST_PAYLOAD_LBA (STAGE2_LBA + STAGE2_SECTORS)

enum manifest_status manifest_validate(const struct manifest *m) {
    if (memcmp(m->magic, MANIFEST_MAGIC, 8) != 0) return MANIFEST_BAD_MAGIC;
    if (m->version != MANIFEST_VERSION)           return MANIFEST_BAD_VERSION;
    if (m->count == 0 || m->count > MANIFEST_MAX_ENTRIES) return MANIFEST_BAD_COUNT;
    for (unsigned i = 0; i < m->count; i++) {
        const struct manifest_entry *e = &m->entries[i];
        if (e->name[MANIFEST_NAME_LEN - 1] != '\0') return MANIFEST_BAD_ENTRY; /* must be terminated */
        if ((uint64_t)e->sectors * 512u < e->size_bytes) return MANIFEST_BAD_ENTRY;
        /* stage2 lives in its fixed slot; everything else must be after it */
        if (strncmp(e->name, "stage2", MANIFEST_NAME_LEN) != 0 && e->lba < FIRST_PAYLOAD_LBA)
            return MANIFEST_BAD_ENTRY;
    }
    return MANIFEST_OK;
}

const struct manifest_entry *manifest_find(const struct manifest *m, const char *name) {
    for (unsigned i = 0; i < m->count && i < MANIFEST_MAX_ENTRIES; i++)
        if (strncmp(m->entries[i].name, name, MANIFEST_NAME_LEN) == 0)
            return &m->entries[i];
    return 0;
}

const char *manifest_status_str(enum manifest_status s) {
    switch (s) {
    case MANIFEST_OK:          return "ok";
    case MANIFEST_BAD_MAGIC:   return "bad magic";
    case MANIFEST_BAD_VERSION: return "bad version";
    case MANIFEST_BAD_COUNT:   return "bad entry count";
    case MANIFEST_BAD_ENTRY:   return "bad entry";
    }
    return "unknown";
}
