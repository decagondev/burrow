#include "image.h"
#include "crc32.h"

enum image_status image_read(struct block_device *dev, const struct manifest_entry *e,
                             void *dest, uint64_t dest_size_bytes) {
    if ((uint64_t)e->sectors * 512 > dest_size_bytes) return IMAGE_TOO_BIG;
    if (block_read(dev, e->lba, e->sectors, dest) != 0) return IMAGE_READ_ERROR;
    if (crc32_ieee(dest, e->size_bytes) != e->crc32) return IMAGE_CRC_MISMATCH;
    return IMAGE_OK;
}

const char *image_status_str(enum image_status s) {
    static const char *const names[] = { "ok", "image too big", "disk read failed", "CRC mismatch" };
    return (unsigned)s < sizeof names / sizeof names[0] ? names[s] : "unknown";
}
