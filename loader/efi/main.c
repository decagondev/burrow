/*
 * main.c - Burrow's UEFI loader (W26): the firmware calls efi_main.
 *
 * S01: hello on COM1 (the markers the smokes read) and the firmware's
 * console, where the firmware put this image, its base relocations applied.
 * S02: the nucleus's fixed pages claimed, the memory map as E820 records at
 * E820_PHYS_ADDR, the ACPI RSDP, the manifest and every payload read from
 * the boot partition with its CRC. S03: the nucleus loaded, its page tables
 * and boot_info v4 through the BIOS loader's back half (loader/handoff.c),
 * then ExitBootServices, interrupts off, and the jump. S04: the screen from
 * GOP (gop.c) in boot_info, and every line on it too (common/fbcon.c).
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
#include "lib_string.h"
#include "cpu.h"
#include "handoff.h"
#include "vmsetup.h"
#include "fbcon.h"

#define MSR_APIC_BASE 0x1Bu

struct efi_system_table *g_st;
efi_handle g_image;
static uint16_t g_com1;
static struct fbcon_tee g_tee;         /* S04: COM1, and the screen once GOP gave one */
static struct fbcon g_con;
static const struct video_request *g_video;   /* the image's "video" payload, if any */
static struct handoff g_handoff;
static struct palloc g_frames;
static struct e820_entry g_arena;      /* the page tables' block, palloc's one "RAM" range */

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

/* Every payload checked; the nucleus's image returned, the rest become boot
 * modules where the firmware's pages hold them. */
static struct efi_payload read_payloads(const struct manifest *m) {
    struct efi_payload nucleus = { 0, 0, 0 };
    for (unsigned i = 0; i < m->count; i++) {
        struct efi_payload p = efi_read_payload(&m->entries[i]);
        if (strncmp(p.name, "nucleus", MANIFEST_NAME_LEN) == 0) nucleus = p;
        else if (!handoff_is_boot_stage(p.name)) handoff_add_module(&g_handoff, p.name, p.phys, p.size_bytes);
        if (strncmp(p.name, "video", MANIFEST_NAME_LEN) == 0 && p.size_bytes == sizeof(struct video_request) &&
            memcmp((const void *)(uintptr_t)p.phys, VIDEO_MAGIC, 4) == 0)
            g_video = (const struct video_request *)(uintptr_t)p.phys;
    }
    if (!nucleus.name) efi_panic("no nucleus in the manifest", EFI_NOT_FOUND);
    return nucleus;
}

/* What stage2 fills on the BIOS path; the rest is handoff_boot_info's. */
static struct boot_info *start_boot_info(uint32_t e820_count, uint64_t rsdp, const struct efi_loaded_image *li) {
    struct boot_info *bi = (struct boot_info *)(uintptr_t)BOOTINFO_PHYS_ADDR;
    memset(bi, 0, sizeof *bi);
    bi->magic = BOOTINFO_MAGIC;
    bi->size = sizeof *bi;
    bi->e820_addr = E820_PHYS_ADDR;
    bi->e820_count = e820_count;
    bi->loader_base = (uint64_t)(uintptr_t)li->image_base;
    bi->rsdp_addr = rsdp;
    return bi;   /* manifest_addr 0: the manifest stays in the boot partition; fb_*: gop.c */
}

/* The page tables come from one block of the firmware's pages below 1 GiB,
 * sized from the physmap (a table per 2 MiB of edges, a directory per GiB). */
static void table_frames(const struct e820_entry *map, uint32_t n) {
    uint64_t bytes = (32 + 2 * (vm_physmap_size(map, n) >> 30)) * EFI_PAGE_BYTES;
    uint64_t base = efi_alloc_low(bytes, "page tables");
    g_arena = (struct e820_entry){ base, bytes, E820_USABLE, 1 };
    palloc_init(&g_frames, &g_arena, 1, base, base + bytes);
    g_handoff.frames = &g_frames;
}

/* PIT-97: what the firmware left behind, for BOOT_PROTOCOL.md's audit. */
static void report_cpu_state(void) {
    kprintf("[efi] machine: cr0=%llx cr4=%llx efer=%llx apic_base=%llx pic masks=%x/%x\n",
            (unsigned long long)read_cr0(), (unsigned long long)read_cr4(),
            (unsigned long long)rdmsr(MSR_EFER), (unsigned long long)rdmsr(MSR_APIC_BASE),
            inb(0x21), inb(0xA1));
}

efi_status EFIAPI efi_main(efi_handle image, struct efi_system_table *st) {
    g_com1 = COM1_PORT;
    serial_init(g_com1);
    fbcon_tee_init(&g_tee, serial_sink(&g_com1));
    kprintf_set_sink(fbcon_tee_sink(&g_tee));
    g_st = st;
    g_image = image;
    if (!st || st->hdr.signature != EFI_SYSTEM_TABLE_SIGNATURE) efi_panic("bad system table", 0);
    st->boot_services->set_watchdog_timer(0, 0, 0, 0);   /* the firmware's 5-minute reset */
    st->con_out->output_string(st->con_out, u"Burrow: UEFI loader\r\n");
    const struct efi_loaded_image *li = say_hello();
    kprintf("BURROW:EFI:OK\n");          /* the UEFI application runs where the firmware put it */

    claim_fixed_pages(li);
    struct e820_entry *e820 = (struct e820_entry *)(uintptr_t)E820_PHYS_ADDR;
    uint32_t n = efi_read_memory_map(e820, E820_MAX_ENTRIES);
    struct boot_info *bi = start_boot_info(n, efi_find_rsdp(), li);
    g_handoff.tag = "efi";
    g_handoff.map = e820;
    g_handoff.map_count = n;
    g_handoff.loader_base = (uint64_t)(uintptr_t)li->image_base;
    g_handoff.loader_bytes = li->image_size;
    struct efi_payload img = read_payloads(efi_read_manifest());
    kprintf("[efi] payloads ok\n");
    struct fbcon_screen screen;          /* S04: the lines so far, and every one after, on it */
    if (efi_setup_screen(bi, g_video, &screen) && fbcon_init(&g_con, &screen)) fbcon_tee_attach(&g_tee, &g_con);

    struct elf_loaded nucleus;           /* S03: the BIOS loader's back half */
    handoff_load_nucleus(&g_handoff, (const uint8_t *)(uintptr_t)img.phys, img.size_bytes, &nucleus);
    table_frames(e820, n);
    uint64_t pml4 = handoff_page_tables(&g_handoff, &nucleus);
    handoff_boot_info(&g_handoff, bi, &nucleus);
    uint64_t physmap_bytes = vm_physmap_size(e820, n);
    kprintf("[efi] leaving boot services\n");

    bi->e820_count = n = efi_leave_boot_services(e820, E820_MAX_ENTRIES);
    kprintf("[efi] boot services exited (%u memory records)\n", n);
    if (vm_physmap_size(e820, n) != physmap_bytes) efi_panic("the last memory map moved the physmap", 0);
    report_cpu_state();
    kprintf("[efi] jumping to nucleus\n");
    handoff_enter(pml4, &nucleus, bi);
}
