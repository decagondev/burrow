/* ata_pio.h - legacy IDE/ATA, PIO mode, polling, 28-bit LBA.
 * Slow and simple: perfect for a loader. QEMU: -drive ...,if=ide */
#ifndef LOADER_ATA_PIO_H
#define LOADER_ATA_PIO_H
#include "block_device.h"

struct ata_channel { uint16_t io; uint16_t ctrl; uint8_t slave; };

/* Fills *dev so it reads from the given channel. Returns 0 if a drive answered. */
int ata_pio_open(struct ata_channel *ch, struct block_device *dev);
#endif
