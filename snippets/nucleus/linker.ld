/* linker.ld - nucleus lives in the top 2 GiB (required by -mcmodel=kernel).
 * Loaded at PHYSICAL 1 MiB by the loader, executed at VIRTUAL 0xFFFFFFFF80100000. */
OUTPUT_FORMAT(elf64-x86-64)
ENTRY(nucleus_entry)
KERNEL_VMA = 0xFFFFFFFF80000000;

SECTIONS
{
    . = KERNEL_VMA + 0x100000;
    __nucleus_start = .;
    .text   ALIGN(4K) : AT(ADDR(.text)   - KERNEL_VMA) { *(.text.entry) *(.text .text.*) }
    .rodata ALIGN(4K) : AT(ADDR(.rodata) - KERNEL_VMA) { *(.rodata .rodata.*) }
    .data   ALIGN(4K) : AT(ADDR(.data)   - KERNEL_VMA) { *(.data .data.*) }
    .bss    ALIGN(4K) : AT(ADDR(.bss)    - KERNEL_VMA) { __bss_start = .; *(.bss .bss.*) *(COMMON) __bss_end = .; }
    __nucleus_end = .;
    /DISCARD/ : { *(.eh_frame*) *(.comment) *(.note*) }
}
