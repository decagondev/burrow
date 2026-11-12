/*
 * main.c - Burrow's UEFI loader (W26): the firmware calls efi_main.
 *
 * S01: say hello on COM1 (the markers the smokes read) and on the firmware's
 * console, show where the firmware put this image and that its base
 * relocations were applied, then leave through QEMU's debug-exit. Later
 * slices read the payloads, leave the boot services and start the nucleus.
 *
 * RULE: firmware is called only through the EFIAPI pointers of efi.h; the
 * loader never writes to the firmware's memory or tables.
 * Docs: docs/04-plan/waves/WAVE-26-uefi.md, docs/02-architecture/BOOT_PROTOCOL.md.
 */
#include <stdint.h>
#include "efi.h"
#include "io.h"
#include "serial.h"
#include "kprintf.h"

static uint16_t g_com1;
static struct efi_system_table *g_st;

/* A pointer the linker stores in data, so it needs a base relocation once
 * the firmware loads the image elsewhere; volatile keeps the compiler from
 * folding it into a RIP-relative address. */
static const char k_probe[] = "probe";
static const char *volatile k_probe_ptr = k_probe;

static void __attribute__((noreturn)) fail(const char *what, efi_status st) {
    kprintf("[efi] FAIL: %s (status %llx)\n", what, (unsigned long long)st);
    qemu_exit(QEMU_EXIT_FAILURE);
    cpu_halt_forever();
}

/* Firmware strings are UTF-16; the serial line gets their ASCII part. */
static void ascii_of(const char16 *s, char *out, size_t cap) {
    size_t i = 0;
    for (; s && s[i] && i + 1 < cap; i++) out[i] = s[i] < 0x80 ? (char)s[i] : '?';
    out[i] = 0;
}

static void say_hello(efi_handle image) {
    static const struct efi_guid loaded_image_guid = EFI_LOADED_IMAGE_PROTOCOL_GUID;
    struct efi_loaded_image *li = 0;
    efi_status st = g_st->boot_services->handle_protocol(image, &loaded_image_guid, (void **)&li);
    if (st != EFI_SUCCESS || !li) fail("no loaded image protocol", st);
    char vendor[40];
    ascii_of(g_st->firmware_vendor, vendor, sizeof vendor);
    kprintf("[efi] hello from UEFI (firmware %s %x, revision %x)\n", vendor,
            g_st->firmware_revision, g_st->hdr.revision);
    kprintf("[efi] image at %p, %llu bytes\n", li->image_base, (unsigned long long)li->image_size);
    if (k_probe_ptr != k_probe) fail("base relocations not applied", 0);
    kprintf("[efi] relocations ok\n");
}

efi_status EFIAPI efi_main(efi_handle image, struct efi_system_table *st) {
    g_com1 = COM1_PORT;
    serial_init(g_com1);
    kprintf_set_sink(serial_sink(&g_com1));
    g_st = st;
    if (!st || st->hdr.signature != EFI_SYSTEM_TABLE_SIGNATURE) fail("bad system table", 0);
    st->boot_services->set_watchdog_timer(0, 0, 0, 0);   /* the firmware's 5-minute reset */
    st->con_out->output_string(st->con_out, u"Burrow: UEFI loader\r\n");
    say_hello(image);
    kprintf("BURROW:EFI:OK\n");
    qemu_exit(QEMU_EXIT_SUCCESS);
    cpu_halt_forever();
}
