/* image.h - read one manifest entry from a block_device and verify its CRC.
 * Depends only on the block_device interface, so host tests use a RAM disk. */
#ifndef LOADER_IMAGE_H
#define LOADER_IMAGE_H
#include <stdint.h>
#include <burrow/manifest.h>
#include "block_device.h"

enum image_status {
    IMAGE_OK = 0,
    IMAGE_TOO_BIG,        /* entry's sectors do not fit in dest_size_bytes */
    IMAGE_READ_ERROR,
    IMAGE_CRC_MISMATCH,
};

/* Reads e->sectors whole sectors into dest, then checks the CRC of the first
 * e->size_bytes. dest must hold e->sectors * 512 bytes. */
enum image_status image_read(struct block_device *dev, const struct manifest_entry *e,
                             void *dest, uint64_t dest_size_bytes);
const char *image_status_str(enum image_status s);
#endif
