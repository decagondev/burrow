/*
 * efi_loader.h - what the UEFI loader's files share (W26).
 *
 * main.c runs the steps; firmware.c asks the firmware about the machine
 * (pages, the memory map, the ACPI tables); files.c reads the boot
 * partition; gop.c sets the screen (S04). Each step prints a marker on COM1
 * (tests/smoke/expect/boot_to_efi.txt), and on the screen once it is set,
 * and any failure is loud: "[efi] PANIC: ..." and debug-exit 0x11.
 * Docs: docs/04-plan/waves/WAVE-26-uefi.md, docs/02-architecture/BOOT_PROTOCOL.md.
 */
#ifndef LOADER_EFI_LOADER_H
#define LOADER_EFI_LOADER_H
#include <stdint.h>
#include <burrow/boot_info.h>
#include "efi.h"
#include "manifest.h"

/* Payloads go below this, as on the BIOS path (stage2's identity map). */
#define EFI_PAYLOAD_LIMIT   0x40000000ull
/* The nucleus's fixed addresses the loader claims from the firmware. */
#define EFI_TRAMPOLINE_PHYS 0x8000ull       /* nucleus/smp.c TRAMPOLINE_PA */
#define EFI_NUCLEUS_PHYS    0x100000ull     /* loader/layout.h NUCLEUS_PHYS_BASE */
#define EFI_NUCLEUS_LIMIT   0x400000ull     /* loader/layout.h NUCLEUS_PHYS_LIMIT */
#define EFI_PAGE_BYTES      4096ull

extern struct efi_system_table *g_st;
extern efi_handle g_image;

void __attribute__((noreturn)) efi_panic(const char *what, efi_status st);

/* firmware.c */
void efi_claim(uint64_t phys, uint64_t bytes, const char *what);
uint64_t efi_alloc_low(uint64_t bytes, const char *what);
uint32_t efi_read_memory_map(struct e820_entry *out, uint32_t cap);
uint64_t efi_find_rsdp(void);
uint32_t efi_leave_boot_services(struct e820_entry *out, uint32_t cap);   /* S03 */

/* files.c: a payload read from burrow/<name> and checked against the manifest. */
struct efi_payload { const char *name; uint64_t phys; uint64_t size_bytes; };
const struct manifest *efi_read_manifest(void);
struct efi_payload efi_read_payload(const struct manifest_entry *e);
int efi_file_exists(const char16 *path);

/* gop.c (S04): the screen's mode set (want may be 0) and its framebuffer
 * written into bi; 1 with *out filled when there is one to draw on. */
struct fbcon_screen;
int efi_setup_screen(struct boot_info *bi, const struct video_request *want, struct fbcon_screen *out);
#endif
