/*
 * efimap.c - UEFI memory descriptors as E820 records (W26-S02). Pure logic:
 * no firmware calls, no I/O; host-tested by tests/unit/c/test_efimap.c.
 * Never trusts the descriptor size to be 40: firmware may add fields.
 * Docs: efimap.h, docs/04-plan/waves/WAVE-26-uefi.md.
 */
#include "efimap.h"

uint32_t efimap_e820_type(uint32_t efi_type) {
    switch (efi_type) {
    case EFIMAP_LOADER_CODE: case EFIMAP_LOADER_DATA:   /* the loader reports what it keeps as modules */
    case EFIMAP_BS_CODE: case EFIMAP_BS_DATA:           /* the firmware's, until ExitBootServices */
    case EFIMAP_CONVENTIONAL:
        return E820_USABLE;
    case EFIMAP_ACPI_RECLAIM: return E820_ACPI;
    case EFIMAP_ACPI_NVS:     return E820_NVS;
    case EFIMAP_UNUSABLE:     return E820_BAD;
    }
    return E820_RESERVED;   /* runtime services, MMIO, PAL code, persistent, unaccepted, unknown */
}

/* Puts one record in its place by base (the firmware's order is not a promise). */
static void insert_sorted(struct e820_entry *out, uint32_t n, struct e820_entry e) {
    uint32_t i = n;
    while (i > 0 && out[i - 1].base > e.base) {
        out[i] = out[i - 1];
        i--;
    }
    out[i] = e;
}

/* Merges records of one type that touch; returns the new count. */
static uint32_t merge(struct e820_entry *out, uint32_t n) {
    uint32_t w = 0;
    for (uint32_t r = 0; r < n; r++) {
        if (w > 0 && out[w - 1].type == out[r].type && out[w - 1].base + out[w - 1].length == out[r].base)
            out[w - 1].length += out[r].length;
        else
            out[w++] = out[r];
    }
    return w;
}

enum efimap_status efimap_to_e820(const void *map, uint64_t map_bytes, uint64_t desc_bytes,
                                  struct e820_entry *out, uint32_t cap, uint32_t *count) {
    *count = 0;
    if (desc_bytes < EFIMAP_DESC_MIN || desc_bytes % 8 != 0) return EFIMAP_BAD_DESC_SIZE;
    const uint8_t *p = map;
    uint32_t n = 0;
    for (uint64_t off = 0; off + desc_bytes <= map_bytes; off += desc_bytes) {
        const uint8_t *d = p + off;
        uint32_t type = (uint32_t)d[0] | (uint32_t)d[1] << 8 | (uint32_t)d[2] << 16 | (uint32_t)d[3] << 24;
        uint64_t base = 0, pages = 0;
        for (int i = 7; i >= 0; i--) {
            base = base << 8 | d[8 + i];     /* physical_start at 8 */
            pages = pages << 8 | d[24 + i];  /* number_of_pages at 24 */
        }
        if (pages == 0) continue;
        if (n == cap) {                      /* full: merging may make room */
            n = merge(out, n);
            if (n == cap) return EFIMAP_TOO_MANY;
        }
        struct e820_entry e = { base, pages * EFIMAP_PAGE_BYTES, efimap_e820_type(type), 1 };
        insert_sorted(out, n++, e);
    }
    *count = merge(out, n);
    return EFIMAP_OK;
}

const char *efimap_status_str(enum efimap_status s) {
    switch (s) {
    case EFIMAP_OK:            return "ok";
    case EFIMAP_BAD_DESC_SIZE: return "bad descriptor size";
    case EFIMAP_TOO_MANY:      return "too many memory regions";
    }
    return "unknown";
}
