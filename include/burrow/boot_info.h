/*
 * boot_info.h - The contract between stage2 (asm), the loader (C) and the nucleus (C).
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
 *   Since W26 the UEFI loader (loader/efi/) fills the same struct from the
 *   firmware instead of stage2.
 */
#ifndef BURROW_BOOT_INFO_H
#define BURROW_BOOT_INFO_H

#include <stdint.h>
#include <stddef.h>

#define BOOTINFO_PHYS_ADDR   0x1000u                /* where stage2 builds it  */
#define BOOTINFO_MAGIC       0x4F464E49544F4F42ull  /* "BOOTINFO" little-endian */
#define BOOTINFO_VERSION_STAGE2 2u                /* what stage2 writes (2: the framebuffer, W21-S02) */
#define BOOTINFO_VERSION     4u                     /* what the nucleus gets (4: rsdp_addr, W26-S02) */

/* fb_source: who filled the framebuffer fields (0: nobody, the kernel uses DISPI). */
#define FB_SOURCE_NONE 0u
#define FB_SOURCE_VBE  1u
#define FB_SOURCE_GOP  2u   /* the UEFI loader, from the graphics output protocol (W26-S04) */

#define E820_PHYS_ADDR       0x2000u   /* stage2 stores the raw E820 map here */
#define E820_MAX_ENTRIES     128u
#define MANIFEST_PHYS_ADDR   0x0600u   /* stage2 copies disk LBA 1 here       */
#define BOOT_MODULES_PHYS_ADDR 0x1100u /* loader writes boot_module[] here (v2) */
#define BOOT_MODULES_MAX     16u

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

/* v2: one physical range the loader filled ("nucleus", "loader-frames", ...).
 * The nucleus must not hand these frames out as free memory. */
struct boot_module {
    char     name[16];         /* NUL-padded                                   */
    uint64_t phys_start;       /* 4 KiB aligned                                */
    uint64_t size_bytes;       /* multiple of 4 KiB                            */
} __attribute__((packed));     /* 32 bytes                                     */

struct boot_info {
    uint64_t magic;            /* 0x00  BOOTINFO_MAGIC                         */
    uint32_t version;          /* 0x08  2 from stage2, 3 after the loader      */
    uint32_t size;             /* 0x0C  sizeof(struct boot_info)               */
    uint32_t boot_drive;       /* 0x10  BIOS drive number (DL), e.g. 0x80      */
    uint32_t e820_count;       /* 0x14  number of valid e820 entries           */
    uint64_t e820_addr;        /* 0x18  physical address of e820_entry[]       */
    uint64_t manifest_addr;    /* 0x20  physical address of the disk manifest  */
    uint64_t loader_base;      /* 0x28  physical load address of the loader    */
    /* Framebuffer (W21-S02): stage2 fills these from the VBE mode it set; all
     * zero (fb_source 0) when it set none. The nucleus checks them (fbdesc.c). */
    uint64_t fb_addr;          /* 0x30 */
    uint32_t fb_width;         /* 0x38 */
    uint32_t fb_height;        /* 0x3C */
    uint32_t fb_pitch;         /* 0x40  bytes per scanline                     */
    uint32_t fb_bpp;           /* 0x44  15, 16, 24 or 32                       */
    /* ---- version 2: filled by the loader just before the nucleus hand-off ---- */
    uint64_t physmap_base;     /* 0x48  virtual address of physical 0          */
    uint64_t modules_addr;     /* 0x50  physical address of boot_module[]      */
    uint32_t module_count;     /* 0x58                                         */
    uint32_t reserved0;        /* 0x5C                                         */
    /* ---- version 3 (W21-S02): the framebuffer's colour fields, in VBE's order */
    uint8_t  fb_red_size;      /* 0x60  bits                                   */
    uint8_t  fb_red_pos;       /* 0x61  lowest bit                             */
    uint8_t  fb_green_size;    /* 0x62                                         */
    uint8_t  fb_green_pos;     /* 0x63                                         */
    uint8_t  fb_blue_size;     /* 0x64                                         */
    uint8_t  fb_blue_pos;      /* 0x65                                         */
    uint8_t  fb_source;        /* 0x66  FB_SOURCE_*                            */
    uint8_t  fb_reserved;      /* 0x67                                         */
    /* ---- version 4 (W26-S02): what UEFI firmware tells the loader ---- */
    uint64_t rsdp_addr;        /* 0x68  physical address of the ACPI RSDP; 0: the
                                *       nucleus scans the BIOS areas (stage2's path) */
    uint64_t reserved[2];      /* 0x70  room to grow without moving anything   */
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
_Static_assert(offsetof(struct boot_info, physmap_base)  == 0x48, "abi drift");
_Static_assert(offsetof(struct boot_info, modules_addr)  == 0x50, "abi drift");
_Static_assert(offsetof(struct boot_info, module_count)  == 0x58, "abi drift");
_Static_assert(offsetof(struct boot_info, fb_width)      == 0x38, "abi drift");
_Static_assert(offsetof(struct boot_info, fb_pitch)      == 0x40, "abi drift");
_Static_assert(offsetof(struct boot_info, fb_bpp)        == 0x44, "abi drift");
_Static_assert(offsetof(struct boot_info, fb_red_size)   == 0x60, "abi drift");
_Static_assert(offsetof(struct boot_info, fb_source)     == 0x66, "abi drift");
_Static_assert(offsetof(struct boot_info, rsdp_addr)     == 0x68, "abi drift");
_Static_assert(offsetof(struct boot_info, reserved)      == 0x70, "abi drift");
_Static_assert(sizeof(struct boot_module)                == 32,   "abi drift");
_Static_assert(BOOT_MODULES_PHYS_ADDR + BOOT_MODULES_MAX * 32 <= 0x2000, "modules overlap e820");
_Static_assert(sizeof(struct boot_info)                  == 0x80, "abi drift");
_Static_assert(sizeof(struct e820_entry)                 == 24,   "abi drift");
#endif

#endif /* BURROW_BOOT_INFO_H */
