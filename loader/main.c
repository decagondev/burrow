/*
 * main.c - Burrow loader (Wave 4 scope).
 *
 * Checks every input it was given, loads the "nucleus" ELF from disk, builds
 * the nucleus's page tables, upgrades boot_info to v2 and jumps to the higher
 * half. Each step prints a stable marker line that tests/smoke/ greps for.
 *
 * Later waves extend this file by calling NEW modules (Open/Closed):
 *   W6: load "kernel" (Go) ELF and "initrd", hand everything to the nucleus
 *   W26-S03: the back half (nucleus, page tables, boot_info, the jump) is
 *   loader/handoff.c, which the UEFI loader shares
 *
 * RULE: keep all mutable globals zero-initialised (.bss). The self-CRC check
 * below hashes the loaded image, so .data must not change before it runs.
 */
#include <stdint.h>
#include <burrow/boot_info.h>
#include "io.h"
#include "serial.h"
#include "kprintf.h"
#include "lib_string.h"
#include "memmap.h"
#include "manifest.h"
#include "crc32.h"
#include "ata_pio.h"
#include "layout.h"
#include "palloc.h"
#include "image.h"
#include "handoff.h"
#include "cpu.h"

extern char __loader_start[], __loader_image_end[], __bss_end[];

static uint16_t g_com1;                 /* .bss: serial_sink keeps a pointer to it */
static struct ata_channel g_ata0;
static struct block_device g_disk;
static uint8_t g_sector[512];
static struct palloc g_palloc;
static struct handoff g_handoff;        /* the back half, shared with the UEFI loader (W26-S03) */

/* An image read from disk into palloc'd frames (identity-mapped, so the
 * physical address is also a usable pointer). */
struct disk_image { const uint8_t *bytes; uint64_t size_bytes; };

static void __attribute__((noreturn)) panic(const char *what) {
    kprintf("[loader] PANIC: %s\n", what);
    qemu_exit(QEMU_EXIT_FAILURE);
    cpu_halt_forever();
}

static void check_boot_info(const struct boot_info *bi) {
    if (bi->magic != BOOTINFO_MAGIC)     panic("boot_info magic mismatch");
    if (bi->version != BOOTINFO_VERSION_STAGE2) panic("boot_info version mismatch");
    if (bi->size != sizeof *bi)          panic("boot_info size mismatch");
    kprintf("[loader] boot_info ok (drive=0x%x, e820 entries=%u)\n",
            bi->boot_drive, bi->e820_count);
}

static void print_memory_map(const struct boot_info *bi) {
    const struct e820_entry *e = (const struct e820_entry *)(uintptr_t)bi->e820_addr;
    for (uint32_t i = 0; i < bi->e820_count; i++)
        kprintf("  %016llx - %016llx  %s\n", (unsigned long long)e[i].base,
                (unsigned long long)(e[i].base + e[i].length), e820_type_str(e[i].type));
    uint64_t usable = e820_usable_bytes(e, bi->e820_count);
    if (usable < 32ull * 1024 * 1024) panic("need at least 32 MiB of RAM");
    kprintf("[loader] memory ok (%llu MiB usable)\n", (unsigned long long)(usable >> 20));
}

static const struct manifest *check_manifest(const struct boot_info *bi) {
    const struct manifest *m = (const struct manifest *)(uintptr_t)bi->manifest_addr;
    enum manifest_status st = manifest_validate(m);
    if (st != MANIFEST_OK) panic(manifest_status_str(st));
    for (unsigned i = 0; i < m->count; i++)
        kprintf("  %s lba=%u sectors=%u bytes=%llu crc=%08x\n", m->entries[i].name,
                m->entries[i].lba, m->entries[i].sectors,
                (unsigned long long)m->entries[i].size_bytes, m->entries[i].crc32);
    kprintf("[loader] manifest ok (%u entries)\n", m->count);
    return m;
}

/* Hash our own bytes in RAM and compare with what mkimage recorded.
 * Catches: truncated loads, wrong LBA, stage2 chunking bugs. */
static void check_self_crc(const struct manifest *m) {
    const struct manifest_entry *me = manifest_find(m, "loader");
    if (!me) panic("no loader entry");
    uint64_t in_ram = (uint64_t)(__loader_image_end - __loader_start);
    if (in_ram != me->size_bytes) panic("loader size differs from manifest");
    uint32_t crc = crc32_ieee(__loader_start, me->size_bytes);
    if (crc != me->crc32) panic("loader CRC mismatch");
    kprintf("[loader] self-crc ok (%08x)\n", crc);
}

/* Read LBA 1 through our own ATA driver and compare with stage2's BIOS copy.
 * Two independent paths agreeing = good evidence both work. */
static void check_disk(const struct boot_info *bi) {
    g_ata0.io = 0x1F0; g_ata0.ctrl = 0x3F6; g_ata0.slave = 0;
    if (ata_pio_open(&g_ata0, &g_disk) != 0) panic("no ATA drive on primary master");
    if (block_read(&g_disk, MANIFEST_LBA, 1, g_sector) != 0) panic("ATA read failed");
    if (memcmp(g_sector, (const void *)(uintptr_t)bi->manifest_addr, 512) != 0)
        panic("ATA data differs from BIOS data");
    kprintf("[loader] ata ok\n");
}

/* Frames for images and page tables: usable RAM in [1 MiB, 1 GiB), except the
 * window the nucleus is linked to load into. */
static void init_frames(const struct boot_info *bi) {
    palloc_init(&g_palloc, (const struct e820_entry *)(uintptr_t)bi->e820_addr, bi->e820_count,
                PALLOC_FLOOR, LOADER_IDENTITY_LIMIT);
    if (palloc_reserve(&g_palloc, NUCLEUS_PHYS_BASE, NUCLEUS_PHYS_LIMIT - NUCLEUS_PHYS_BASE) != 0)
        panic("palloc reserve table full");
}

/* Read one manifest entry into fresh frames and verify it ("verify before you
 * execute", BUG-0003). Failure text is "<name> CRC mismatch" etc. (REG-0004). */
static struct disk_image read_payload(const struct manifest_entry *e) {
    uint64_t buf_bytes = ((uint64_t)e->sectors * 512 + PAGE_SIZE - 1) & ~(uint64_t)(PAGE_SIZE - 1);
    uint64_t pa = buf_bytes ? palloc_alloc(&g_palloc, buf_bytes / PAGE_SIZE) : 0;
    enum image_status st = pa ? image_read(&g_disk, e, (void *)(uintptr_t)pa, buf_bytes) : IMAGE_TOO_BIG;
    if (st != IMAGE_OK) {
        kprintf("[loader] PANIC: %s %s\n", e->name, pa ? image_status_str(st) : "out of memory");
        qemu_exit(QEMU_EXIT_FAILURE);
        cpu_halt_forever();
    }
    kprintf("[loader] %s image crc ok (%llu bytes at phys %llx)\n", e->name,
            (unsigned long long)e->size_bytes, (unsigned long long)pa);
    struct disk_image img = { (const uint8_t *)(uintptr_t)pa, e->size_bytes };
    return img;
}

static struct disk_image read_nucleus_image(const struct manifest *m) {
    const struct manifest_entry *e = manifest_find(m, "nucleus");
    if (!e) panic("no nucleus entry in manifest");
    return read_payload(e);
}

/* Every payload that is not a boot stage ("ktest", "kernel", "initrd", ...)
 * is read, verified and handed over as a boot module. */
static void load_payload_modules(const struct manifest *m) {
    for (unsigned i = 0; i < m->count; i++) {
        const struct manifest_entry *e = &m->entries[i];
        if (handoff_is_boot_stage(e->name)) continue;
        struct disk_image img = read_payload(e);
        handoff_add_module(&g_handoff, e->name, (uint64_t)(uintptr_t)img.bytes, img.size_bytes);
    }
}

void __attribute__((noreturn)) loader_main(struct boot_info *bi) {
    g_com1 = COM1_PORT;
    serial_init(g_com1);
    kprintf_set_sink(serial_sink(&g_com1));

    kprintf("[loader] hello from C (image %p..%p, end %p)\n",
            (void *)__loader_start, (void *)__loader_image_end, (void *)__bss_end);

    check_boot_info(bi);
    print_memory_map(bi);
    const struct manifest *m = check_manifest(bi);
    check_self_crc(m);
    check_disk(bi);

    init_frames(bi);
    g_handoff.tag = "loader";
    g_handoff.frames = &g_palloc;
    g_handoff.map = (const struct e820_entry *)(uintptr_t)bi->e820_addr;
    g_handoff.map_count = bi->e820_count;
    struct elf_loaded nucleus;
    struct disk_image img = read_nucleus_image(m);
    handoff_load_nucleus(&g_handoff, img.bytes, img.size_bytes, &nucleus);
    load_payload_modules(m);
    uint64_t pml4 = handoff_page_tables(&g_handoff, &nucleus);
    bi->rsdp_addr = 0;                  /* v4: the nucleus scans the BIOS areas */
    handoff_boot_info(&g_handoff, bi, &nucleus);   /* last: loader-frames covers the page tables */

    kprintf("[loader] all checks passed\n");
    kprintf("BURROW:LOADER:OK\n");      /* smoke-test success marker */
    kprintf("[loader] jumping to nucleus\n");
    handoff_enter(pml4, &nucleus, bi);
}
