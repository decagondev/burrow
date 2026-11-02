/* linker.ld - nucleus lives in the top 2 GiB (required by -mcmodel=kernel).
 * Loaded at PHYSICAL 1 MiB by the loader, executed at VIRTUAL 0xFFFFFFFF80100000.
 * Explicit PHDRS give one PT_LOAD per permission set, so the loader can map
 * text R+X, rodata R, data+bss R+W (W^X, PIT-20). See MEMORY_MAP.md §2. */
OUTPUT_FORMAT(elf64-x86-64)
ENTRY(nucleus_entry)
KERNEL_VMA = 0xFFFFFFFF80000000;

PHDRS
{
    text   PT_LOAD FLAGS(5);   /* R X */
    rodata PT_LOAD FLAGS(4);   /* R   */
    data   PT_LOAD FLAGS(6);   /* R W */
}

SECTIONS
{
    . = KERNEL_VMA + 0x100000;
    __nucleus_start = .;
    .text   ALIGN(4K) : AT(ADDR(.text)   - KERNEL_VMA) { *(.text.entry) *(.text .text.*) } :text
    .rodata ALIGN(4K) : AT(ADDR(.rodata) - KERNEL_VMA) {
        *(.rodata .rodata.*)
        . = ALIGN(8);
        __fixup_start = .; KEEP(*(.fixup)) __fixup_end = .;   /* trap.h: faults allowed here */
    } :rodata
    .data   ALIGN(4K) : AT(ADDR(.data)   - KERNEL_VMA) { *(.data .data.*) } :data
    .bss    ALIGN(4K) : AT(ADDR(.bss)    - KERNEL_VMA) { __bss_start = .; *(.bss .bss.*) *(COMMON) __bss_end = .; } :data
    __nucleus_end = .;
    /DISCARD/ : { *(.eh_frame*) *(.comment) *(.note*) }
}
