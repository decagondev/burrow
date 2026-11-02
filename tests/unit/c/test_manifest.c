#include "minitest.h"
#include "manifest.h"

static struct manifest good(void) {
    struct manifest m;
    memset(&m, 0, sizeof m);
    memcpy(m.magic, "BRWMNFST", 8);
    m.version = 1; m.count = 2;
    strcpy(m.entries[0].name, "stage2"); m.entries[0].lba = 2;  m.entries[0].sectors = 32; m.entries[0].size_bytes = 16384;
    strcpy(m.entries[1].name, "loader"); m.entries[1].lba = 34; m.entries[1].sectors = 12; m.entries[1].size_bytes = 6000;
    return m;
}

TEST(manifest_valid)      { struct manifest m = good(); CHECK(manifest_validate(&m) == MANIFEST_OK); }
TEST(manifest_bad_magic)  { struct manifest m = good(); m.magic[0] = 'X'; CHECK(manifest_validate(&m) == MANIFEST_BAD_MAGIC); }
TEST(manifest_bad_count)  { struct manifest m = good(); m.count = 12; CHECK(manifest_validate(&m) == MANIFEST_BAD_COUNT); }
TEST(manifest_too_small)  { struct manifest m = good(); m.entries[1].sectors = 1; CHECK(manifest_validate(&m) == MANIFEST_BAD_ENTRY); }
TEST(manifest_overlap_stage2) { struct manifest m = good(); m.entries[1].lba = 10; CHECK(manifest_validate(&m) == MANIFEST_BAD_ENTRY); }
TEST(manifest_find) {
    struct manifest m = good();
    CHECK(manifest_find(&m, "loader") == &m.entries[1]);
    CHECK(manifest_find(&m, "kernel") == 0);
}
