/*
 * firmware.c - what the UEFI loader asks the firmware (W26-S02): pages at
 * the nucleus's fixed addresses, pages for payloads below 1 GiB, the memory
 * map as E820 records (efimap.c), and the ACPI RSDP from the configuration
 * table.
 *
 * RULE: only boot services, only before ExitBootServices; a page the
 * firmware will not give is a loud failure naming the address, never a
 * silent move (PIT-94).
 * Docs: efi_loader.h, docs/04-plan/waves/WAVE-26-uefi.md.
 */
#include "efi_loader.h"
#include "efimap.h"
#include "kprintf.h"
#include "lib_string.h"
#include "memmap.h"

static uint64_t pages_of(uint64_t bytes) { return (bytes + EFI_PAGE_BYTES - 1) / EFI_PAGE_BYTES; }

void efi_claim(uint64_t phys, uint64_t bytes, const char *what) {
    uint64_t addr = phys;
    efi_status st = g_st->boot_services->allocate_pages(EFI_ALLOCATE_ADDRESS, EFI_LOADER_DATA, pages_of(bytes), &addr);
    if (st != EFI_SUCCESS) {
        kprintf("[efi] PANIC: the firmware owns %llx-%llx (%s)\n", (unsigned long long)phys,
                (unsigned long long)(phys + bytes - 1), what);
        efi_panic("a fixed address is taken", st);
    }
    kprintf("[efi] claimed %llx-%llx (%s)\n", (unsigned long long)phys, (unsigned long long)(phys + bytes - 1), what);
}

uint64_t efi_alloc_low(uint64_t bytes, const char *what) {
    uint64_t addr = EFI_PAYLOAD_LIMIT - 1;
    efi_status st = g_st->boot_services->allocate_pages(EFI_ALLOCATE_MAX_ADDRESS, EFI_LOADER_DATA, pages_of(bytes), &addr);
    if (st != EFI_SUCCESS) {
        kprintf("[efi] PANIC: %s out of memory (%llu bytes below 1 GiB)\n", what, (unsigned long long)bytes);
        efi_panic("out of memory", st);
    }
    memset((void *)(uintptr_t)addr, 0, pages_of(bytes) * EFI_PAGE_BYTES);
    return addr;
}

/* The firmware's map, read into a pool buffer with room for the descriptors
 * the allocation itself may add, then converted (efimap.c). */
uint32_t efi_read_memory_map(struct e820_entry *out, uint32_t cap) {
    struct efi_boot_services *bs = g_st->boot_services;
    uint64_t size = 0, key, desc;
    uint32_t version;
    efi_status st = bs->get_memory_map(&size, 0, &key, &desc, &version);
    if (st != EFI_BUFFER_TOO_SMALL) efi_panic("GetMemoryMap gave no size", st);
    size += 8 * desc;
    void *map = 0;
    if ((st = bs->allocate_pool(EFI_LOADER_DATA, size, &map)) != EFI_SUCCESS) efi_panic("no pool for the memory map", st);
    if ((st = bs->get_memory_map(&size, map, &key, &desc, &version)) != EFI_SUCCESS) efi_panic("GetMemoryMap", st);
    uint32_t n;
    enum efimap_status es = efimap_to_e820(map, size, desc, out, cap, &n);
    if (es != EFIMAP_OK) {
        kprintf("[efi] PANIC: memory map: %s\n", efimap_status_str(es));
        efi_panic("memory map", 0);
    }
    for (uint32_t i = 0; i < n; i++)
        kprintf("  %016llx - %016llx  %s\n", (unsigned long long)out[i].base,
                (unsigned long long)(out[i].base + out[i].length), e820_type_str(out[i].type));
    kprintf("[efi] memory map ok (%u records from %u descriptors, %llu MiB usable)\n", n,
            (unsigned)(size / desc), (unsigned long long)(e820_usable_bytes(out, n) >> 20));
    bs->free_pool(map);
    return n;
}

/* ACPI 2.0's table first (it may carry an XSDT), then 1.0's; each checked
 * for the RSDP's signature before it is believed. */
uint64_t efi_find_rsdp(void) {
    static const struct efi_guid acpi20 = EFI_ACPI_20_TABLE_GUID, acpi10 = EFI_ACPI_10_TABLE_GUID;
    const struct efi_guid *want[2] = { &acpi20, &acpi10 };
    const char *names[2] = { "ACPI 2.0", "ACPI 1.0" };
    for (int w = 0; w < 2; w++) {
        for (uint64_t i = 0; i < g_st->number_of_table_entries; i++) {
            const struct efi_configuration_table *t = &g_st->configuration_table[i];
            if (memcmp(&t->vendor_guid, want[w], sizeof *want[w]) != 0) continue;
            if (!t->vendor_table || memcmp(t->vendor_table, "RSD PTR ", 8) != 0) continue;
            kprintf("[efi] acpi rsdp at %llx (%s)\n", (unsigned long long)(uintptr_t)t->vendor_table, names[w]);
            return (uint64_t)(uintptr_t)t->vendor_table;
        }
    }
    efi_panic("no ACPI RSDP in the configuration table", EFI_NOT_FOUND);
}
