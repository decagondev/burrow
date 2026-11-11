/* vdso.lds - how Burrow's vDSO is laid out (W25-S02), after Linux's
 * vdso-layout.lds: one loadable segment from file offset 0 at address 0,
 * so the file's bytes are the image the kernel maps; the dynamic symbol
 * table, hash tables and version definitions musl and Go look for; the
 * data page (vvar_page) one page below the image. Nothing writable, no
 * relocations. Docs: docs/04-plan/waves/WAVE-25-speed.md. */

PROVIDE_HIDDEN(vvar_page = -4096);

SECTIONS
{
    . = SIZEOF_HEADERS;
    .hash           : { *(.hash) }               :text
    .gnu.hash       : { *(.gnu.hash) }
    .dynsym         : { *(.dynsym) }
    .dynstr         : { *(.dynstr) }
    .gnu.version    : { *(.gnu.version) }
    .gnu.version_d  : { *(.gnu.version_d) }
    .gnu.version_r  : { *(.gnu.version_r) }
    .dynamic        : { *(.dynamic) }            :text :dynamic
    .rodata         : { *(.rodata*) }            :text
    .text           : { *(.text*) }              :text
    /DISCARD/       : { *(.data*) *(.bss*) *(.eh_frame*) *(.note*) *(.comment) }
}

PHDRS
{
    text    PT_LOAD    FLAGS(5) FILEHDR PHDRS;   /* read and execute */
    dynamic PT_DYNAMIC FLAGS(4);
}

VERSION
{
    LINUX_2.6 {
    global:
        clock_gettime; __vdso_clock_gettime;
        gettimeofday;  __vdso_gettimeofday;
        time;          __vdso_time;
        getcpu;        __vdso_getcpu;
    local: *;
    };
}
