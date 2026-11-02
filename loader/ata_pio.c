#include "ata_pio.h"
#include "io.h"

enum { REG_DATA = 0, REG_ERROR = 1, REG_COUNT = 2, REG_LBA0 = 3, REG_LBA1 = 4,
       REG_LBA2 = 5, REG_DRIVE = 6, REG_STATUS = 7, REG_CMD = 7 };
#define ST_ERR 0x01
#define ST_DRQ 0x08
#define ST_DF  0x20
#define ST_BSY 0x80
#define CMD_READ_SECTORS 0x20

static void delay400ns(struct ata_channel *ch) {
    for (int i = 0; i < 4; i++) (void)inb(ch->ctrl);  /* each alt-status read ~100ns */
}

static int wait_ready(struct ata_channel *ch) {
    for (uint32_t spins = 0; spins < 10000000u; spins++) {
        uint8_t st = inb(ch->io + REG_STATUS);
        if (st & ST_BSY) continue;
        if (st & (ST_ERR | ST_DF)) return -1;
        if (st & ST_DRQ) return 0;
    }
    return -2;  /* timeout */
}

static int ata_read(struct block_device *dev, uint64_t lba, uint32_t count, void *buf) {
    struct ata_channel *ch = dev->ctx;
    uint16_t *out = buf;
    if (lba + count > 0x0FFFFFFFull) return -3;      /* 28-bit LBA limit (128 GiB) */
    while (count) {
        uint32_t n = count > 255 ? 255 : count;
        outb(ch->io + REG_DRIVE, (uint8_t)(0xE0 | (ch->slave << 4) | ((lba >> 24) & 0x0F)));
        delay400ns(ch);
        outb(ch->io + REG_COUNT, (uint8_t)n);
        outb(ch->io + REG_LBA0, (uint8_t)lba);
        outb(ch->io + REG_LBA1, (uint8_t)(lba >> 8));
        outb(ch->io + REG_LBA2, (uint8_t)(lba >> 16));
        outb(ch->io + REG_CMD, CMD_READ_SECTORS);
        for (uint32_t s = 0; s < n; s++) {
            delay400ns(ch);
            int rc = wait_ready(ch);
            if (rc) return rc;
            for (int w = 0; w < 256; w++) *out++ = inw(ch->io + REG_DATA);
        }
        lba += n;
        count -= n;
    }
    return 0;
}

int ata_pio_open(struct ata_channel *ch, struct block_device *dev) {
    outb(ch->io + REG_DRIVE, (uint8_t)(0xA0 | (ch->slave << 4)));
    delay400ns(ch);
    if (inb(ch->io + REG_STATUS) == 0xFF) return -1;  /* floating bus: nothing there */
    dev->name = "ata-pio";
    dev->read = ata_read;
    dev->ctx  = ch;
    return 0;
}
