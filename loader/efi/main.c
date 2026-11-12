/*
 * main.c - Burrow's UEFI loader (W26): the firmware calls efi_main.
 *
 * S01: hello on COM1 (the markers the smokes read) and the firmware's
 * console, where the firmware put this image, its base relocations applied.
 * S02: the nucleus's fixed pages claimed, the memory map as E820 records at
 * E820_PHYS_ADDR, the ACPI RSDP, the manifest and every payload read from
 * the boot partition with its CRC. Later slices leave the boot services and
 * start the nucleus through the BIOS loader's back half.
 *
 * RULE: firmware is called only through the EFIAPI pointers of efi.h; the
 * loader never writes to the firmware's memory or tables.
 * Docs: docs/04-plan/waves/WAVE-26-uefi.md, docs/02-architecture/BOOT_PROTOCOL.md.
 */
#include <stdint.h>
#include "efi_loader.h"
#include "io.h"
#include "serial.h"
#include "kprintf.h"

struct efi_system_table *g_st;
efi_handle g_image;
static uint16_t g_com1;

/* A pointer the linker stores in data, so it needs a base relocation once
 * the firmware loads the image elsewhere; volatile keeps the compiler from
 * folding it into a RIP-relative address. */
static const char k_probe[] = "probe";
static const char *volatile k_probe_ptr = k_probe;

void __attribute__((noreturn)) efi_panic(const char *what, efi_status st) {
    kprintf("[efi] PANIC: %s (status %llx)\n", what, (unsigned long long)st);
    qemu_exit(QEMU_EXIT_FAILURE);
    cpu_halt_forever();
}

/* Firmware strings are UTF-16; the serial line gets their ASCII part. */
static void ascii_of(const char16 *s, char *out, size_t cap) {
    size_t i = 0;
    for (; s && s[i] && i + 1 < cap; i++) out[i] = s[i] < 0x80 ? (char)s[i] : '?';
    out[i] = 0;
}

static struct efi_loaded_image *say_hello(void) {
    static const struct efi_guid loaded_image_guid = EFI_LOADED_IMAGE_PROTOCOL_GUID;
    struct efi_loaded_image *li = 0;
    efi_status st = g_st->boot_services->handle_protocol(g_image, &loaded_image_guid, (void **)&li);
    if (st != EFI_SUCCESS || !li) efi_panic("no loaded image protocol", st);
    char vendor[40];
    ascii_of(g_st->firmware_vendor, vendor, sizeof vendor);
    kprintf("[efi] hello from UEFI (firmware %s %x, revision %x)\n", vendor,
            g_st->firmware_revision, g_st->hdr.revision);
    kprintf("[efi] image at %p, %llu bytes\n", li->image_base, (unsigned long long)li->image_size);
    if (k_probe_ptr != k_probe) efi_panic("base relocations not applied", 0);
    kprintf("[efi] relocations ok\n");
    return li;
}

/* The pages the nucleus will expect at fixed addresses (MEMORY_MAP.md).
 * burrow/efitest asks for one more claim, of this loader's own first page,
 * which the firmware cannot give: tests/regression's proof that a taken
 * address stops the boot loudly (REG-0011). */
static void claim_fixed_pages(const struct efi_loaded_image *li) {
    efi_claim(BOOTINFO_PHYS_ADDR, 2 * EFI_PAGE_BYTES, "boot_info, modules, e820");
    efi_claim(EFI_TRAMPOLINE_PHYS, EFI_PAGE_BYTES, "ap trampoline");
    efi_claim(EFI_NUCLEUS_PHYS, EFI_NUCLEUS_LIMIT - EFI_NUCLEUS_PHYS, "nucleus");
    if (efi_file_exists(u"\\burrow\\efitest"))
        efi_claim((uint64_t)(uintptr_t)li->image_base, EFI_PAGE_BYTES, "efitest: the loader's own image");
}

static void read_payloads(const struct manifest *m) {
    for (unsigned i = 0; i < m->count; i++) efi_read_payload(&m->entries[i]);
}

efi_status EFIAPI efi_main(efi_handle image, struct efi_system_table *st) {
    g_com1 = COM1_PORT;
    serial_init(g_com1);
    kprintf_set_sink(serial_sink(&g_com1));
    g_st = st;
    g_image = image;
    if (!st || st->hdr.signature != EFI_SYSTEM_TABLE_SIGNATURE) efi_panic("bad system table", 0);
    st->boot_services->set_watchdog_timer(0, 0, 0, 0);   /* the firmware's 5-minute reset */
    st->con_out->output_string(st->con_out, u"Burrow: UEFI loader\r\n");
    const struct efi_loaded_image *li = say_hello();
    kprintf("BURROW:EFI:OK\n");          /* the UEFI application runs where the firmware put it */

    claim_fixed_pages(li);
    struct e820_entry *e820 = (struct e820_entry *)(uintptr_t)E820_PHYS_ADDR;
    efi_read_memory_map(e820, E820_MAX_ENTRIES);
    efi_find_rsdp();
    read_payloads(efi_read_manifest());
    kprintf("[efi] payloads ok\n");

    qemu_exit(QEMU_EXIT_SUCCESS);        /* S03: leave the boot services, start the nucleus */
    cpu_halt_forever();
}
