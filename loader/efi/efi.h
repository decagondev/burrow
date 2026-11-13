/*
 * efi.h - the few UEFI structures Burrow's UEFI loader uses (W26).
 *
 * Declared by hand from the UEFI specification (2.10): the system table, the
 * boot services, the text output, loaded image, simple file system, file and
 * graphics output (W26-S04) protocols, and the GUIDs that name them. No
 * EDK II and no gnu-efi: what is not used is not declared, and the members
 * before a used one are kept only as placeholders so the offsets match the
 * specification.
 *
 * RULE: every function pointer here is EFIAPI (Microsoft's x64 convention);
 * calling firmware through a pointer without it corrupts arguments.
 * Docs: docs/04-plan/waves/WAVE-26-uefi.md, docs/02-architecture/BOOT_PROTOCOL.md.
 */
#ifndef BURROW_EFI_H
#define BURROW_EFI_H
#include <stdint.h>
#include <stddef.h>

#define EFIAPI __attribute__((ms_abi))

typedef uint64_t efi_status;
typedef void *efi_handle;
typedef uint16_t char16;

#define EFI_SUCCESS            0ull
#define EFI_ERROR_BIT          (1ull << 63)
#define EFI_LOAD_ERROR         (EFI_ERROR_BIT | 1)
#define EFI_INVALID_PARAMETER  (EFI_ERROR_BIT | 2)
#define EFI_BUFFER_TOO_SMALL   (EFI_ERROR_BIT | 5)
#define EFI_NOT_FOUND          (EFI_ERROR_BIT | 14)

struct efi_guid {
    uint32_t data1;
    uint16_t data2;
    uint16_t data3;
    uint8_t  data4[8];
};

struct efi_table_header {
    uint64_t signature;
    uint32_t revision;
    uint32_t header_size;
    uint32_t crc32;
    uint32_t reserved;
};

#define EFI_SYSTEM_TABLE_SIGNATURE 0x5453595320494249ull   /* "IBI SYST" */

/* EFI_SIMPLE_TEXT_OUTPUT_PROTOCOL */
struct efi_text_out {
    void *reset;
    efi_status (EFIAPI *output_string)(struct efi_text_out *self, const char16 *s);
    void *test_string, *query_mode, *set_mode, *set_attribute, *clear_screen,
         *set_cursor_position, *enable_cursor, *mode;
};

/* EFI_MEMORY_DESCRIPTOR: firmware may make each one larger (descriptor_size). */
struct efi_memory_descriptor {
    uint32_t type;
    uint32_t pad;
    uint64_t physical_start;
    uint64_t virtual_start;
    uint64_t number_of_pages;
    uint64_t attribute;
};

enum efi_allocate_type { EFI_ALLOCATE_ANY_PAGES, EFI_ALLOCATE_MAX_ADDRESS, EFI_ALLOCATE_ADDRESS };
enum { EFI_LOADER_DATA = 2 };

/* EFI_BOOT_SERVICES, in the specification's order. */
struct efi_boot_services {
    struct efi_table_header hdr;
    void *raise_tpl, *restore_tpl;
    efi_status (EFIAPI *allocate_pages)(int type, int memory_type, uint64_t pages, uint64_t *phys);
    efi_status (EFIAPI *free_pages)(uint64_t phys, uint64_t pages);
    efi_status (EFIAPI *get_memory_map)(uint64_t *map_size, struct efi_memory_descriptor *map,
                                        uint64_t *map_key, uint64_t *desc_size, uint32_t *desc_version);
    efi_status (EFIAPI *allocate_pool)(int memory_type, uint64_t size, void **buffer);
    efi_status (EFIAPI *free_pool)(void *buffer);
    void *create_event, *set_timer, *wait_for_event, *signal_event, *close_event, *check_event;
    void *install_protocol_interface, *reinstall_protocol_interface, *uninstall_protocol_interface;
    efi_status (EFIAPI *handle_protocol)(efi_handle h, const struct efi_guid *protocol, void **iface);
    void *reserved, *register_protocol_notify, *locate_handle, *locate_device_path,
         *install_configuration_table;
    void *load_image, *start_image, *exit, *unload_image;
    efi_status (EFIAPI *exit_boot_services)(efi_handle image, uint64_t map_key);
    void *get_next_monotonic_count, *stall;
    efi_status (EFIAPI *set_watchdog_timer)(uint64_t timeout_s, uint64_t code, uint64_t size, char16 *data);
    void *connect_controller, *disconnect_controller;
    void *open_protocol, *close_protocol, *open_protocol_information;
    void *protocols_per_handle, *locate_handle_buffer;
    efi_status (EFIAPI *locate_protocol)(const struct efi_guid *protocol, void *registration, void **iface);
    void *install_multiple_protocol_interfaces, *uninstall_multiple_protocol_interfaces;
    void *calculate_crc32, *copy_mem, *set_mem, *create_event_ex;
};

struct efi_configuration_table {
    struct efi_guid vendor_guid;
    void *vendor_table;
};

struct efi_system_table {
    struct efi_table_header hdr;
    char16 *firmware_vendor;
    uint32_t firmware_revision;
    efi_handle console_in_handle;
    void *con_in;
    efi_handle console_out_handle;
    struct efi_text_out *con_out;
    efi_handle standard_error_handle;
    struct efi_text_out *std_err;
    void *runtime_services;
    struct efi_boot_services *boot_services;
    uint64_t number_of_table_entries;
    struct efi_configuration_table *configuration_table;
};

/* EFI_LOADED_IMAGE_PROTOCOL */
struct efi_loaded_image {
    uint32_t revision;
    efi_handle parent_handle;
    struct efi_system_table *system_table;
    efi_handle device_handle;
    void *file_path;
    void *reserved;
    uint32_t load_options_size;
    void *load_options;
    void *image_base;
    uint64_t image_size;
    uint32_t image_code_type;
    uint32_t image_data_type;
    void *unload;
};

/* EFI_FILE_PROTOCOL */
struct efi_file {
    uint64_t revision;
    efi_status (EFIAPI *open)(struct efi_file *self, struct efi_file **out, const char16 *name,
                              uint64_t mode, uint64_t attributes);
    efi_status (EFIAPI *close)(struct efi_file *self);
    void *delete_;
    efi_status (EFIAPI *read)(struct efi_file *self, uint64_t *size, void *buffer);
    void *write, *get_position, *set_position;
    efi_status (EFIAPI *get_info)(struct efi_file *self, const struct efi_guid *type, uint64_t *size, void *buffer);
    void *set_info, *flush;
};
#define EFI_FILE_MODE_READ 1ull

/* EFI_SIMPLE_FILE_SYSTEM_PROTOCOL */
struct efi_simple_file_system {
    uint64_t revision;
    efi_status (EFIAPI *open_volume)(struct efi_simple_file_system *self, struct efi_file **root);
};

/* EFI_GRAPHICS_OUTPUT_PROTOCOL (W26-S04). */
enum efi_pixel_format {
    EFI_PIXEL_RGBX_8 = 0,    /* PixelRedGreenBlueReserved8BitPerColor: red in byte 0 */
    EFI_PIXEL_BGRX_8 = 1,    /* PixelBlueGreenRedReserved8BitPerColor: blue in byte 0 */
    EFI_PIXEL_BIT_MASK = 2,  /* PixelBitMask: the masks say */
    EFI_PIXEL_BLT_ONLY = 3,  /* no framebuffer at all */
};

struct efi_pixel_bitmask { uint32_t red, green, blue, reserved; };

struct efi_gop_mode_info {
    uint32_t version;
    uint32_t horizontal_resolution;
    uint32_t vertical_resolution;
    uint32_t pixel_format;              /* enum efi_pixel_format */
    struct efi_pixel_bitmask mask;
    uint32_t pixels_per_scan_line;
};

struct efi_gop_mode {
    uint32_t max_mode;
    uint32_t mode;
    struct efi_gop_mode_info *info;
    uint64_t size_of_info;
    uint64_t frame_buffer_base;
    uint64_t frame_buffer_size;
};

struct efi_gop {
    efi_status (EFIAPI *query_mode)(struct efi_gop *self, uint32_t mode, uint64_t *size_of_info,
                                    struct efi_gop_mode_info **info);
    efi_status (EFIAPI *set_mode)(struct efi_gop *self, uint32_t mode);
    void *blt;
    struct efi_gop_mode *mode;
};

#define EFI_LOADED_IMAGE_PROTOCOL_GUID \
    { 0x5B1B31A1, 0x9562, 0x11D2, { 0x8E, 0x3F, 0x00, 0xA0, 0xC9, 0x69, 0x72, 0x3B } }
#define EFI_SIMPLE_FILE_SYSTEM_PROTOCOL_GUID \
    { 0x964E5B22, 0x6459, 0x11D2, { 0x8E, 0x39, 0x00, 0xA0, 0xC9, 0x69, 0x72, 0x3B } }
#define EFI_FILE_INFO_GUID \
    { 0x09576E92, 0x6D3F, 0x11D2, { 0x8E, 0x39, 0x00, 0xA0, 0xC9, 0x69, 0x72, 0x3B } }
#define EFI_GRAPHICS_OUTPUT_PROTOCOL_GUID \
    { 0x9042A9DE, 0x23DC, 0x4A38, { 0x96, 0xFB, 0x7A, 0xDE, 0xD0, 0x80, 0x51, 0x6A } }
#define EFI_ACPI_20_TABLE_GUID \
    { 0x8868E871, 0xE4F1, 0x11D3, { 0xBC, 0x22, 0x00, 0x80, 0xC7, 0x3C, 0x88, 0x81 } }
#define EFI_ACPI_10_TABLE_GUID \
    { 0xEB9D2D30, 0x2D88, 0x11D3, { 0x9A, 0x16, 0x00, 0x90, 0x27, 0x3F, 0xC1, 0x4D } }

/* Offsets the specification fixes, checked so a missing placeholder shows. */
_Static_assert(offsetof(struct efi_system_table, con_out) == 64, "efi drift");
_Static_assert(offsetof(struct efi_system_table, boot_services) == 96, "efi drift");
_Static_assert(offsetof(struct efi_system_table, configuration_table) == 112, "efi drift");
_Static_assert(offsetof(struct efi_boot_services, allocate_pages) == 40, "efi drift");
_Static_assert(offsetof(struct efi_boot_services, get_memory_map) == 56, "efi drift");
_Static_assert(offsetof(struct efi_boot_services, handle_protocol) == 152, "efi drift");
_Static_assert(offsetof(struct efi_boot_services, exit_boot_services) == 232, "efi drift");
_Static_assert(offsetof(struct efi_boot_services, set_watchdog_timer) == 256, "efi drift");
_Static_assert(offsetof(struct efi_boot_services, locate_protocol) == 320, "efi drift");
_Static_assert(offsetof(struct efi_loaded_image, device_handle) == 24, "efi drift");
_Static_assert(offsetof(struct efi_loaded_image, image_base) == 64, "efi drift");
_Static_assert(sizeof(struct efi_memory_descriptor) == 40, "efi drift");
_Static_assert(sizeof(struct efi_configuration_table) == 24, "efi drift");
_Static_assert(sizeof(struct efi_gop_mode_info) == 36, "efi drift");
_Static_assert(offsetof(struct efi_gop_mode_info, pixels_per_scan_line) == 32, "efi drift");
_Static_assert(offsetof(struct efi_gop_mode, frame_buffer_base) == 24, "efi drift");
_Static_assert(offsetof(struct efi_gop, mode) == 24, "efi drift");
#endif
