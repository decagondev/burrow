/*
 * efimap.h - UEFI memory descriptors as E820 records (W26-S02). Pure logic.
 *
 * The nucleus and the loader's back half know only E820 (boot_info.h), so
 * the UEFI loader turns the firmware's map into that, as the map will be
 * after ExitBootServices: boot-services and loader memory are usable,
 * runtime services, MMIO and anything unknown stay reserved, ACPI tables are
 * E820_ACPI and ACPI NVS E820_NVS. Records come out sorted by base with
 * neighbours of one type merged.
 * Docs: docs/04-plan/waves/WAVE-26-uefi.md, docs/02-architecture/BOOT_PROTOCOL.md.
 */
#ifndef LOADER_EFI_EFIMAP_H
#define LOADER_EFI_EFIMAP_H
#include <stdint.h>
#include <burrow/boot_info.h>

/* UEFI's memory types (EFI_MEMORY_TYPE) the conversion names. */
enum efimap_type {
    EFIMAP_RESERVED = 0, EFIMAP_LOADER_CODE = 1, EFIMAP_LOADER_DATA = 2,
    EFIMAP_BS_CODE = 3, EFIMAP_BS_DATA = 4, EFIMAP_RT_CODE = 5, EFIMAP_RT_DATA = 6,
    EFIMAP_CONVENTIONAL = 7, EFIMAP_UNUSABLE = 8, EFIMAP_ACPI_RECLAIM = 9, EFIMAP_ACPI_NVS = 10,
};

#define EFIMAP_PAGE_BYTES 4096u
#define EFIMAP_DESC_MIN   40u   /* struct efi_memory_descriptor; firmware may use more */

enum efimap_status { EFIMAP_OK = 0, EFIMAP_BAD_DESC_SIZE, EFIMAP_TOO_MANY };

/* The E820 type a descriptor of this UEFI type becomes. */
uint32_t efimap_e820_type(uint32_t efi_type);

/* Converts map_bytes of descriptors (desc_bytes each) into at most cap
 * records in out; *count gets how many. */
enum efimap_status efimap_to_e820(const void *map, uint64_t map_bytes, uint64_t desc_bytes,
                                  struct e820_entry *out, uint32_t cap, uint32_t *count);
const char *efimap_status_str(enum efimap_status s);
#endif
