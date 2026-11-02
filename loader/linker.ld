/* linker.ld - loader is a flat binary loaded at physical 0x10000 by stage2. */
OUTPUT_FORMAT(elf64-x86-64)
ENTRY(_start)

SECTIONS
{
    . = 0x10000;
    __loader_start = .;

    .text : {
        *(.text.entry)          /* MUST be first: stage2 jumps to 0x10000 */
        *(.text .text.*)
    }
    .rodata : { *(.rodata .rodata.*) }
    .data   : { *(.data .data.*) }
    __loader_image_end = .;     /* end of bytes that exist in loader.bin */

    .bss (NOLOAD) : {
        __bss_start = .;
        *(.bss .bss.*) *(COMMON)
        __bss_end = .;
    }
    __loader_end = .;

    /DISCARD/ : { *(.eh_frame*) *(.comment) *(.note*) }
}
