/*
 * files.c - the boot partition through the firmware's file protocol
 * (W26-S02): burrow/manifest, then each payload it lists from burrow/<name>,
 * read into pages below 1 GiB and checked for its size and CRC before
 * anything uses it ("verify before you execute", BUG-0003), with the same
 * failure text as the BIOS loader ("<name> CRC mismatch", REG-0004).
 *
 * RULE: reads only, and only from the volume the loader itself came from.
 * Docs: efi_loader.h, docs/04-plan/waves/WAVE-26-uefi.md.
 */
#include "efi_loader.h"
#include "kprintf.h"
#include "lib_string.h"
#include "crc32.h"

#define PATH_CHARS 48

static struct efi_file *g_root;
static uint8_t g_manifest[1024];   /* a manifest is one sector; room to see a longer file */

static struct efi_file *root(void) {
    if (g_root) return g_root;
    static const struct efi_guid li_guid = EFI_LOADED_IMAGE_PROTOCOL_GUID, fs_guid = EFI_SIMPLE_FILE_SYSTEM_PROTOCOL_GUID;
    struct efi_boot_services *bs = g_st->boot_services;
    struct efi_loaded_image *li = 0;
    struct efi_simple_file_system *fs = 0;
    efi_status st = bs->handle_protocol(g_image, &li_guid, (void **)&li);
    if (st != EFI_SUCCESS) efi_panic("no loaded image protocol", st);
    if ((st = bs->handle_protocol(li->device_handle, &fs_guid, (void **)&fs)) != EFI_SUCCESS)
        efi_panic("the boot device has no file system", st);
    if ((st = fs->open_volume(fs, &g_root)) != EFI_SUCCESS) efi_panic("OpenVolume", st);
    return g_root;
}

/* \burrow\<name> as UTF-16. */
static void payload_path(const char *name, char16 *out) {
    static const char prefix[] = "\\burrow\\";
    size_t n = 0;
    for (size_t i = 0; prefix[i]; i++) out[n++] = (char16)prefix[i];
    for (size_t i = 0; name[i] && n + 1 < PATH_CHARS; i++) out[n++] = (char16)name[i];
    out[n] = 0;
}

/* Reads up to *size bytes of path into buf; *size gets how many. */
static efi_status read_file(const char16 *path, void *buf, uint64_t *size) {
    struct efi_file *f = 0;
    efi_status st = root()->open(root(), &f, path, EFI_FILE_MODE_READ, 0);
    if (st != EFI_SUCCESS) return st;
    st = f->read(f, size, buf);
    f->close(f);
    return st;
}

int efi_file_exists(const char16 *path) {
    struct efi_file *f = 0;
    if (root()->open(root(), &f, path, EFI_FILE_MODE_READ, 0) != EFI_SUCCESS) return 0;
    f->close(f);
    return 1;
}

const struct manifest *efi_read_manifest(void) {
    uint64_t size = sizeof g_manifest;
    efi_status st = read_file(u"\\burrow\\manifest", g_manifest, &size);
    if (st != EFI_SUCCESS) efi_panic("no burrow/manifest on the boot partition", st);
    const struct manifest *m = (const struct manifest *)g_manifest;
    enum manifest_status ms = size == 512 ? manifest_validate(m) : MANIFEST_BAD_COUNT;
    if (ms != MANIFEST_OK) {
        kprintf("[efi] PANIC: manifest: %s (%llu bytes)\n", manifest_status_str(ms), (unsigned long long)size);
        efi_panic("bad manifest", 0);
    }
    for (unsigned i = 0; i < m->count; i++)
        kprintf("  %s bytes=%llu crc=%08x\n", m->entries[i].name,
                (unsigned long long)m->entries[i].size_bytes, m->entries[i].crc32);
    kprintf("[efi] manifest ok (%u entries)\n", m->count);
    return m;
}

/* One byte more than the manifest says is read, so a longer file shows. */
struct efi_payload efi_read_payload(const struct manifest_entry *e) {
    char16 path[PATH_CHARS];
    payload_path(e->name, path);
    uint64_t phys = efi_alloc_low(e->size_bytes + 1, e->name);
    uint64_t size = e->size_bytes + 1;
    efi_status st = read_file(path, (void *)(uintptr_t)phys, &size);
    const char *bad = st != EFI_SUCCESS ? "missing"
                    : size != e->size_bytes ? "size differs from manifest"
                    : crc32_ieee((const void *)(uintptr_t)phys, size) != e->crc32 ? "CRC mismatch" : 0;
    if (bad) {
        kprintf("[efi] PANIC: %s %s\n", e->name, bad);
        efi_panic("bad payload", st);
    }
    kprintf("[efi] %s image crc ok (%llu bytes at phys %llx)\n", e->name,
            (unsigned long long)size, (unsigned long long)phys);
    struct efi_payload p = { e->name, phys, size };
    return p;
}
