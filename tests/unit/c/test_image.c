/* image_read against a RAM disk that implements block_device. */
#include "minitest.h"
#include "image.h"
#include "crc32.h"

static unsigned char disk[16 * 512];
static int fail_reads;

static int ram_read(struct block_device *dev, uint64_t lba, uint32_t count, void *buf) {
    (void)dev;
    if (fail_reads || (lba + count) * 512 > sizeof disk) return -1;
    memcpy(buf, disk + lba * 512, count * 512);
    return 0;
}
static struct block_device ram = { "ram", ram_read, 0 };

static struct manifest_entry entry_for(uint32_t lba, uint64_t size_bytes) {
    struct manifest_entry e = { "nucleus", lba, (uint32_t)((size_bytes + 511) / 512), size_bytes, 0, 0 };
    for (unsigned i = 0; i < sizeof disk; i++) disk[i] = (unsigned char)(i * 7);
    e.crc32 = crc32_ieee(disk + lba * 512, size_bytes);
    fail_reads = 0;
    return e;
}

TEST(image_reads_and_verifies) {
    static unsigned char dest[4 * 512];
    struct manifest_entry e = entry_for(3, 1000);      /* 2 sectors, partial last one */
    CHECK(image_read(&ram, &e, dest, sizeof dest) == IMAGE_OK);
    CHECK(memcmp(dest, disk + 3 * 512, 1000) == 0);
}
TEST(image_detects_corruption) {
    static unsigned char dest[4 * 512];
    struct manifest_entry e = entry_for(3, 1000);
    disk[3 * 512 + 999] ^= 0xFF;                       /* last byte that counts */
    CHECK(image_read(&ram, &e, dest, sizeof dest) == IMAGE_CRC_MISMATCH);
    CHECK_STR(image_status_str(IMAGE_CRC_MISMATCH), "CRC mismatch");
}
TEST(image_rejects_small_dest_and_read_errors) {
    static unsigned char dest[4 * 512];
    struct manifest_entry e = entry_for(0, 1025);      /* 3 sectors */
    CHECK(image_read(&ram, &e, dest, 1024) == IMAGE_TOO_BIG);
    fail_reads = 1;
    CHECK(image_read(&ram, &e, dest, sizeof dest) == IMAGE_READ_ERROR);
}
