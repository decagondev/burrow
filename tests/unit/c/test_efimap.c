#include <string.h>
#include "minitest.h"
#include "efimap.h"

/* One firmware descriptor, written as the firmware lays it out (desc bytes each). */
static void put_desc(uint8_t *map, unsigned i, unsigned desc, uint32_t type, uint64_t base, uint64_t pages) {
    uint8_t *d = map + i * desc;
    memset(d, 0, desc);
    memcpy(d, &type, 4);
    memcpy(d + 8, &base, 8);
    memcpy(d + 24, &pages, 8);
}

TEST(efimap_types) {
    CHECK_EQ_U64(efimap_e820_type(EFIMAP_CONVENTIONAL), E820_USABLE);
    CHECK_EQ_U64(efimap_e820_type(EFIMAP_BS_DATA), E820_USABLE);
    CHECK_EQ_U64(efimap_e820_type(EFIMAP_LOADER_CODE), E820_USABLE);
    CHECK_EQ_U64(efimap_e820_type(EFIMAP_RT_DATA), E820_RESERVED);
    CHECK_EQ_U64(efimap_e820_type(EFIMAP_ACPI_RECLAIM), E820_ACPI);
    CHECK_EQ_U64(efimap_e820_type(EFIMAP_ACPI_NVS), E820_NVS);
    CHECK_EQ_U64(efimap_e820_type(EFIMAP_UNUSABLE), E820_BAD);
    CHECK_EQ_U64(efimap_e820_type(11), E820_RESERVED);   /* MMIO */
    CHECK_EQ_U64(efimap_e820_type(0x80000000u), E820_RESERVED);   /* an OEM type */
}

/* A map in OVMF's shape, out of order, 48-byte descriptors: neighbours of one
 * E820 type merge, a hole or a type change splits, empty ones vanish. */
TEST(efimap_merges_sorts) {
    uint8_t map[48 * 8];
    put_desc(map, 0, 48, EFIMAP_CONVENTIONAL, 0x100000, 0x700);   /* 1 MiB .. 8 MiB */
    put_desc(map, 1, 48, EFIMAP_BS_CODE, 0x0, 0xA0);              /* 0 .. 640 KiB */
    put_desc(map, 2, 48, EFIMAP_ACPI_NVS, 0x800000, 0x8);
    put_desc(map, 3, 48, EFIMAP_LOADER_DATA, 0x808000, 0x10);
    put_desc(map, 4, 48, EFIMAP_CONVENTIONAL, 0x818000, 0x100);
    put_desc(map, 5, 48, EFIMAP_RT_DATA, 0xFFC00000, 0x400);
    put_desc(map, 6, 48, EFIMAP_CONVENTIONAL, 0x900000, 0);       /* empty */
    put_desc(map, 7, 48, EFIMAP_ACPI_RECLAIM, 0x7F00000, 0x10);
    struct e820_entry e[8];
    uint32_t n = 99;
    CHECK(efimap_to_e820(map, sizeof map, 48, e, 8, &n) == EFIMAP_OK);
    CHECK_EQ_U64(n, 6);
    CHECK_EQ_U64(e[0].base, 0x0);       CHECK_EQ_U64(e[0].length, 0xA0000);  CHECK_EQ_U64(e[0].type, E820_USABLE);
    CHECK_EQ_U64(e[1].base, 0x100000);  CHECK_EQ_U64(e[1].length, 0x700000); /* not merged across the hole */
    CHECK_EQ_U64(e[2].type, E820_NVS);
    CHECK_EQ_U64(e[3].base, 0x808000);  CHECK_EQ_U64(e[3].length, 0x110000); CHECK_EQ_U64(e[3].type, E820_USABLE);
    CHECK_EQ_U64(e[4].type, E820_ACPI); CHECK_EQ_U64(e[4].base, 0x7F00000);
    CHECK_EQ_U64(e[5].base, 0xFFC00000); CHECK_EQ_U64(e[5].type, E820_RESERVED);
    CHECK_EQ_U64(e[5].acpi_ext, 1);
}

/* More descriptors than records fit, but they merge: fine; ones that do not: refused. */
TEST(efimap_capacity) {
    uint8_t map[40 * 6];
    for (unsigned i = 0; i < 6; i++) put_desc(map, i, 40, EFIMAP_CONVENTIONAL, 0x1000ull * i, 1);
    struct e820_entry e[2];
    uint32_t n;
    CHECK(efimap_to_e820(map, sizeof map, 40, e, 2, &n) == EFIMAP_OK);
    CHECK_EQ_U64(n, 1);
    CHECK_EQ_U64(e[0].length, 0x6000);
    for (unsigned i = 0; i < 6; i++) put_desc(map, i, 40, i % 2 ? EFIMAP_RT_CODE : EFIMAP_CONVENTIONAL, 0x1000ull * i, 1);
    CHECK(efimap_to_e820(map, sizeof map, 40, e, 2, &n) == EFIMAP_TOO_MANY);
    CHECK(efimap_to_e820(map, sizeof map, 36, e, 2, &n) == EFIMAP_BAD_DESC_SIZE);
    CHECK_STR(efimap_status_str(EFIMAP_TOO_MANY), "too many memory regions");
}
