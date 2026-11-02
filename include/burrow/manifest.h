/*
 * manifest.h - On-disk table of contents (disk LBA 1).
 *
 * Written by tools/mkimage (Go, host side). Read by stage2 (asm) and the
 * loader (C). Mirrors tools/mkimage/manifest.go and boot/boot_abi.inc.
 *
 * Disk layout (one sector = 512 bytes):
 *   LBA 0        stage1 (MBR, ends with 0x55 0xAA)
 *   LBA 1        this manifest
 *   LBA 2..33    stage2 (fixed 32-sector slot = 16 KiB)
 *   LBA 34..     payload images listed in entries[] (loader, nucleus, kernel, initrd...)
 */
#ifndef BURROW_MANIFEST_H
#define BURROW_MANIFEST_H

#include <stdint.h>
#include <stddef.h>

#define MANIFEST_LBA         1u
#define MANIFEST_MAGIC       "BRWMNFST"   /* 8 bytes, no NUL stored */
#define MANIFEST_VERSION     1u
#define MANIFEST_MAX_ENTRIES 11u
#define MANIFEST_NAME_LEN    16u

#define STAGE2_LBA           2u
#define STAGE2_SECTORS       32u

struct manifest_entry {
    char     name[MANIFEST_NAME_LEN]; /* NUL-padded, e.g. "loader"            */
    uint32_t lba;                     /* first sector on disk                 */
    uint32_t sectors;                 /* sectors occupied (rounded up)        */
    uint64_t size_bytes;              /* exact byte size of the image         */
    uint32_t crc32;                   /* IEEE CRC-32 of the exact bytes       */
    uint32_t flags;                   /* reserved, 0                          */
} __attribute__((packed));            /* 40 bytes */

struct manifest {
    char     magic[8];                /* "BRWMNFST"                           */
    uint16_t version;
    uint16_t count;                   /* valid entries                        */
    uint32_t reserved;
    struct manifest_entry entries[MANIFEST_MAX_ENTRIES];
} __attribute__((packed));            /* 16 + 11*40 = 456 bytes (<= 512)      */

_Static_assert(sizeof(struct manifest_entry) == 40, "abi drift");
_Static_assert(sizeof(struct manifest) <= 512, "manifest must fit one sector");

#endif /* BURROW_MANIFEST_H */
