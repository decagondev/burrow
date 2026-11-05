; boot_abi.inc - NASM mirror of include/burrow/boot_info.h and manifest.h
; Keep in sync! The C side has _Static_asserts; this side has your eyes.

; ---- physical memory map used during boot (see docs/02-architecture/MEMORY_MAP.md)
MANIFEST_ADDR      equ 0x0600      ; copy of disk LBA 1
BOOTINFO_ADDR      equ 0x1000      ; struct boot_info
E820_ADDR          equ 0x2000      ; struct e820_entry[E820_MAX]
E820_MAX           equ 128
PML4_ADDR          equ 0x3000      ; boot page tables (identity map, 1 GiB)
PDPT_ADDR          equ 0x4000
PD_ADDR            equ 0x5000
STAGE1_ADDR        equ 0x7C00
STAGE2_ADDR        equ 0x7E00
LOADER_SEG         equ 0x1000      ; loader loaded at 0x1000:0000 = 0x10000
LOADER_ADDR        equ 0x10000
LOADER_MAX_SECT    equ 512         ; 256 KiB cap
LOADER_STACK_TOP   equ 0x80000
; VBE scratch (W21-S01), between stage2's slot and the loader; free after boot
VBE_INFO_ADDR      equ 0xC000      ; VbeInfoBlock, 512 B (4F00)
VBE_MODE_ADDR      equ 0xC200      ; ModeInfoBlock of the chosen mode, 256 B (4F01)
VBE_REQ_ADDR       equ 0xC400      ; the "video" payload's sector, 512 B

; ---- disk layout
MANIFEST_LBA       equ 1
STAGE2_LBA         equ 2
STAGE2_SECTORS     equ 32

; ---- struct boot_info offsets
BI_MAGIC           equ 0x00
BI_VERSION         equ 0x08
BI_SIZE            equ 0x0C
BI_BOOT_DRIVE      equ 0x10
BI_E820_COUNT      equ 0x14
BI_E820_ADDR       equ 0x18
BI_MANIFEST_ADDR   equ 0x20
BI_LOADER_BASE     equ 0x28
BI_STRUCT_SIZE     equ 0x80
BOOTINFO_VERSION   equ 1          ; = BOOTINFO_VERSION_STAGE2; the loader upgrades to 2
; v2 fields: written by the loader, never by stage2 (stage2 zeroes them)
BI_PHYSMAP_BASE    equ 0x48
BI_MODULES_ADDR    equ 0x50
BI_MODULE_COUNT    equ 0x58
; "BOOTINFO" as two dwords (little-endian): 'BOOT' then 'INFO'
BOOTINFO_MAGIC_LO  equ 0x544F4F42
BOOTINFO_MAGIC_HI  equ 0x4F464E49

; ---- struct manifest offsets
MF_MAGIC           equ 0
MF_VERSION         equ 8
MF_COUNT           equ 10
MF_ENTRIES         equ 16
ME_SIZE            equ 40          ; sizeof(struct manifest_entry)
ME_NAME            equ 0
ME_LBA             equ 16
ME_SECTORS         equ 20
ME_SIZE_BYTES      equ 24
ME_CRC32           equ 32

; ---- struct video_request offsets (manifest.h; the "video" payload, W21)
VR_MAGIC           equ 0
VR_WIDTH           equ 4
VR_HEIGHT          equ 6
VR_BPP             equ 8
VR_PITCH           equ 12
VIDEO_MAGIC_DW     equ 0x52444956  ; "VIDR" little-endian

; ---- serial (COM1)
COM1               equ 0x3F8

; ---- QEMU isa-debug-exit device (tests only; harmless on real hardware)
DEBUG_EXIT_PORT    equ 0xF4
