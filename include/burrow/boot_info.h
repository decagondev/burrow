/*
 * boot_info.h - The contract between stage2 (asm) and the loader (C).
 *
 * RULE: this file and boot/boot_abi.inc describe the SAME bytes.
 * If you change one, change the other, bump BOOTINFO_VERSION, and the
 * _Static_asserts below will tell you if the offsets drifted.
 *
 * Why a struct at a fixed address instead of registers?
 *   - It is easy to inspect in a debugger (x/16gx 0x1000).
 *   - It grows without changing calling conventions (add fields, bump version).
 *   - It is the single source of truth handed up the chain:
 *       stage2 -> loader -> nucleus -> Go kernel.
 */
#ifndef BURROW_BOOT_INFO_H
#define BURROW_BOOT_INFO_H

#include <stdint.h>
#include <stddef.h>

#define BOOTINFO_PHYS_ADDR   0x1000u                /* where stage2 builds it  */
#define BOOTINFO_MAGIC       0x4F464E49544F4F42ull  /* "BOOTINFO" little-endian */
#define BOOTINFO_VERSION     1u

#define E820_PHYS_ADDR       0x2000u   /* stage2 stores the raw E820 map here */
#define E820_MAX_ENTRIES     128u
#define MANIFEST_PHYS_ADDR   0x0600u   /* stage2 copies disk LBA 1 here       */

/* E820 memory types (ACPI spec). */
enum e820_type {
    E820_USABLE   = 1,
    E820_RESERVED = 2,
    E820_ACPI     = 3,
    E820_NVS      = 4,
    E820_BAD      = 5,
};

/* One BIOS E820 record. 24 bytes because we ask the BIOS for the ACPI 3.0 form. */
struct e820_entry {
    uint64_t base;
    uint64_t length;
    uint32_t type;
    uint32_t acpi_ext;
} __attribute__((packed));

struct boot_info {
    uint64_t magic;            /* 0x00  BOOTINFO_MAGIC                         */
    uint32_t version;          /* 0x08  BOOTINFO_VERSION                       */
    uint32_t size;             /* 0x0C  sizeof(struct boot_info)               */
    uint32_t boot_drive;       /* 0x10  BIOS drive number (DL), e.g. 0x80      */
    uint32_t e820_count;       /* 0x14  number of valid e820 entries           */
    uint64_t e820_addr;        /* 0x18  physical address of e820_entry[]       */
    uint64_t manifest_addr;    /* 0x20  physical address of the disk manifest  */
    uint64_t loader_base;      /* 0x28  physical load address of the loader    */
    /* Framebuffer: zero until a later wave fills it (VBE or BGA). */
    uint64_t fb_addr;          /* 0x30 */
    uint32_t fb_width;         /* 0x38 */
    uint32_t fb_height;        /* 0x3C */
    uint32_t fb_pitch;         /* 0x40 */
    uint32_t fb_bpp;           /* 0x44 */
    uint64_t reserved[7];      /* 0x48  room to grow without moving anything   */
} __attribute__((packed));     /* total 0x80 = 128 bytes                       */

#ifndef __ASSEMBLER__
_Static_assert(offsetof(struct boot_info, magic)         == 0x00, "abi drift");
_Static_assert(offsetof(struct boot_info, version)       == 0x08, "abi drift");
_Static_assert(offsetof(struct boot_info, boot_drive)    == 0x10, "abi drift");
_Static_assert(offsetof(struct boot_info, e820_count)    == 0x14, "abi drift");
_Static_assert(offsetof(struct boot_info, e820_addr)     == 0x18, "abi drift");
_Static_assert(offsetof(struct boot_info, manifest_addr) == 0x20, "abi drift");
_Static_assert(offsetof(struct boot_info, loader_base)   == 0x28, "abi drift");
_Static_assert(offsetof(struct boot_info, fb_addr)       == 0x30, "abi drift");
_Static_assert(sizeof(struct boot_info)                  == 0x80, "abi drift");
_Static_assert(sizeof(struct e820_entry)                 == 24,   "abi drift");
#endif

#endif /* BURROW_BOOT_INFO_H */
