/* block_device.h - abstraction for "something that reads 512-byte sectors".
 * The loader's image-loading code depends on THIS, not on ATA. Swap in AHCI
 * or virtio-blk later without touching the loading logic (Open/Closed). */
#ifndef LOADER_BLOCK_DEVICE_H
#define LOADER_BLOCK_DEVICE_H
#include <stdint.h>

struct block_device {
    const char *name;
    /* returns 0 on success, negative on error */
    int (*read)(struct block_device *dev, uint64_t lba, uint32_t count, void *buf);
    void *ctx;
};

static inline int block_read(struct block_device *d, uint64_t lba, uint32_t n, void *buf) {
    return d->read(d, lba, n, buf);
}
#endif
