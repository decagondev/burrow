/* linker.ld - Burrow's UEFI loader as a static position-independent ELF (W26-S01).
 *
 * mkimage -efi-app turns it into a PE32+ image: each PT_LOAD becomes a section
 * at the same relative address (so segments start page-aligned above the PE
 * headers), and each R_X86_64_RELATIVE in .rela.dyn a base relocation. Code
 * and data are on separate pages so the firmware can map them W^X.
 * Docs: docs/04-plan/waves/WAVE-26-uefi.md. */
OUTPUT_FORMAT(elf64-x86-64)
ENTRY(efi_main)

PHDRS
{
    text PT_LOAD FLAGS(5);      /* r-x */
    data PT_LOAD FLAGS(6);      /* rw- */
    dynamic PT_DYNAMIC FLAGS(6);
}

SECTIONS
{
    . = 0x1000;                 /* the PE headers take the first page */
    .text : { *(.text .text.*) } :text
    . = ALIGN(0x1000);
    .rodata : { *(.rodata .rodata.*) } :data
    .data.rel.ro : { *(.data.rel.ro .data.rel.ro.*) } :data
    .data : { *(.data .data.*) } :data
    .got : { *(.got .got.plt) } :data
    .dynamic : { *(.dynamic) } :data :dynamic
    .rela.dyn : { *(.rela.dyn .rela.*) } :data
    .dynsym : { *(.dynsym) } :data
    .dynstr : { *(.dynstr) } :data
    .hash : { *(.hash .gnu.hash) } :data
    .bss : { *(.bss .bss.*) *(COMMON) } :data

    /DISCARD/ : { *(.eh_frame*) *(.comment) *(.note*) *(.interp) }
}
