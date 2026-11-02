/* memmap.h - helpers over the BIOS E820 table. Pure logic. */
#ifndef LOADER_MEMMAP_H
#define LOADER_MEMMAP_H
#include <stdint.h>
#include <burrow/boot_info.h>

const char *e820_type_str(uint32_t type);
uint64_t    e820_usable_bytes(const struct e820_entry *e, uint32_t count);
/* Highest end address of any usable region (useful to size the physmap later). */
uint64_t    e820_max_usable_addr(const struct e820_entry *e, uint32_t count);
#endif
