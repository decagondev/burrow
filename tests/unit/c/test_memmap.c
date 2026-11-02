#include "minitest.h"
#include "memmap.h"

static const struct e820_entry map[] = {
    { 0x0,      0x9FC00,   E820_USABLE,   1 },
    { 0x9FC00,  0x400,     E820_RESERVED, 1 },
    { 0x100000, 0x7EE0000, E820_USABLE,   1 },
};
TEST(e820_usable_sum) { CHECK_EQ_U64(e820_usable_bytes(map, 3), 0x9FC00 + 0x7EE0000); }
TEST(e820_max_addr)   { CHECK_EQ_U64(e820_max_usable_addr(map, 3), 0x7FE0000); }
TEST(e820_names)      { CHECK_STR(e820_type_str(E820_NVS), "acpi-nvs"); CHECK_STR(e820_type_str(99), "unknown"); }
