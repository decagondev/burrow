#include "memmap.h"

const char *e820_type_str(uint32_t type) {
    switch (type) {
    case E820_USABLE:   return "usable";
    case E820_RESERVED: return "reserved";
    case E820_ACPI:     return "acpi-reclaim";
    case E820_NVS:      return "acpi-nvs";
    case E820_BAD:      return "bad";
    default:            return "unknown";
    }
}

uint64_t e820_usable_bytes(const struct e820_entry *e, uint32_t count) {
    uint64_t total = 0;
    for (uint32_t i = 0; i < count; i++)
        if (e[i].type == E820_USABLE) total += e[i].length;
    return total;
}

uint64_t e820_max_usable_addr(const struct e820_entry *e, uint32_t count) {
    uint64_t max = 0;
    for (uint32_t i = 0; i < count; i++)
        if (e[i].type == E820_USABLE && e[i].base + e[i].length > max)
            max = e[i].base + e[i].length;
    return max;
}
