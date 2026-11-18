/* acpi: the RSDP found where firmware puts it, RSDT and XSDT, the MADT's
 * processors (disabled ones left out) and I/O APIC, the FADT's ports, and
 * \_S5 in the DSDT's AML in the forms QEMU and others write it (in an SSDT
 * and every integer encoding since W28-S04, with the reset register); tables
 * with bad checksums skipped. The tables are built here, byte by byte, as
 * the spec lays them out (W20-S04). */
#include <stdlib.h>
#include <string.h>
#include "minitest.h"
#include "acpi.h"

#define MEM_BYTES 0x200000u   /* 2 MiB of "physical memory" */

static uint8_t *g_mem;

static const void *map(void *ctx, uint64_t pa, uint64_t len) {
    (void)ctx;
    return pa + len <= MEM_BYTES ? g_mem + pa : 0;
}

static const struct acpi_mem g_m = {map, 0, 0};

static void put16(uint64_t pa, uint16_t v) { memcpy(g_mem + pa, &v, 2); }
static void put32(uint64_t pa, uint32_t v) { memcpy(g_mem + pa, &v, 4); }
static void put64(uint64_t pa, uint64_t v) { memcpy(g_mem + pa, &v, 8); }

static void sum_fix(uint64_t pa, uint32_t len, uint32_t at) {
    uint8_t s = 0;
    g_mem[pa + at] = 0;
    for (uint32_t i = 0; i < len; i++) s += g_mem[pa + i];
    g_mem[pa + at] = (uint8_t)-s;
}

/* header writes a table header of len bytes and fixes its checksum later */
static void header(uint64_t pa, const char *sig, uint32_t len) {
    memcpy(g_mem + pa, sig, 4);
    put32(pa + 4, len);
    g_mem[pa + 8] = 1;
}

static void rsdp(uint64_t pa, int rev, uint32_t rsdt, uint64_t xsdt) {
    memcpy(g_mem + pa, "RSD PTR ", 8);
    g_mem[pa + 15] = (uint8_t)rev;
    put32(pa + 16, rsdt);
    sum_fix(pa, 20, 8);
    if (rev >= 2) {
        put32(pa + 20, 36);
        put64(pa + 24, xsdt);
        sum_fix(pa, 36, 32);
    }
}

/* madt: four CPUs (apic ids 0..3), one disabled (9), an I/O APIC */
static uint32_t madt(uint64_t pa, int override) {
    uint32_t off = 44;
    for (int i = 0; i < 5; i++, off += 8) {
        g_mem[pa + off] = 0;              /* processor local APIC */
        g_mem[pa + off + 1] = 8;
        g_mem[pa + off + 2] = (uint8_t)i;
        g_mem[pa + off + 3] = (uint8_t)(i < 4 ? i : 9);
        put32(pa + off + 4, i < 4 ? 1 : 0);
    }
    g_mem[pa + off] = 1;                  /* I/O APIC */
    g_mem[pa + off + 1] = 12;
    g_mem[pa + off + 2] = 7;
    put32(pa + off + 4, 0xFEC00000u);
    put32(pa + off + 8, 0);               /* its first GSI */
    off += 12;
    static const struct { uint8_t bus, src; uint32_t gsi; uint16_t flags; } ovr[] = {
        {0, 0, 2, 0},                     /* QEMU's: the PIT on GSI 2 */
        {0, 9, 9, 0x0D},                  /* the SCI, level, active high */
        {1, 4, 20, 0x0F},                 /* not ISA: ignored */
    };
    for (unsigned k = 0; k < 3; k++, off += 10) {
        g_mem[pa + off] = 2;              /* interrupt source override */
        g_mem[pa + off + 1] = 10;
        g_mem[pa + off + 2] = ovr[k].bus;
        g_mem[pa + off + 3] = ovr[k].src;
        put32(pa + off + 4, ovr[k].gsi);
        put16(pa + off + 8, ovr[k].flags);
    }
    if (override) {
        g_mem[pa + off] = 5;              /* local APIC address override */
        g_mem[pa + off + 1] = 12;
        put64(pa + off + 4, 0xFEE10000ull);
        off += 12;
    }
    header(pa, "APIC", off);
    put32(pa + 36, 0xFEE00000u);
    sum_fix(pa, off, 9);
    return off;
}

static void fadt(uint64_t pa, uint32_t dsdt, uint64_t x_dsdt) {
    uint32_t len = x_dsdt ? 244 : 116;
    header(pa, "FACP", len);
    put32(pa + 40, dsdt);
    put16(pa + 46, 9);                    /* SCI */
    put32(pa + 48, 0xB2);                 /* SMI_CMD */
    g_mem[pa + 52] = 0xF1;                /* ACPI_ENABLE */
    put32(pa + 64, 0x604);                /* PM1a_CNT_BLK */
    if (x_dsdt) put64(pa + 140, x_dsdt);
    sum_fix(pa, len, 9);
}

static void dsdt(uint64_t pa, const uint8_t *aml, uint32_t n) {
    memcpy(g_mem + pa + 36, aml, n);
    header(pa, "DSDT", 36 + n);
    sum_fix(pa, 36 + n, 9);
}

static void fresh(void) {
    if (!g_mem) g_mem = malloc(MEM_BYTES);
    memset(g_mem, 0, MEM_BYTES);
}

/* QEMU's pc machine: an ACPI 1.0 RSDP in the BIOS area, an RSDT, and
 * Name(_S5, Package(4){0, 0, 0, 0}). */
TEST(acpi_qemu_like) {
    fresh();
    static const uint8_t aml[] = {0x10, 0x05, 0x08, '_', 'S', '5', '_', 0x12, 0x06, 0x04, 0x00, 0x00, 0x00, 0x00};
    dsdt(0x100000, aml, sizeof aml);
    fadt(0x101000, 0x100000, 0);
    madt(0x102000, 0);
    header(0x103000, "RSDT", 44);
    put32(0x103000 + 36, 0x101000);
    put32(0x103000 + 40, 0x102000);
    sum_fix(0x103000, 44, 9);
    rsdp(0xF5A10, 0, 0x103000, 0);
    struct acpi_info i;
    CHECK(acpi_parse(&g_m, &i) == ACPI_OK);
    CHECK(i.revision == 0 && i.ncpus == 4);
    CHECK(i.apic_id[0] == 0 && i.apic_id[3] == 3);
    CHECK_EQ_U64(i.lapic_pa, 0xFEE00000u);
    CHECK_EQ_U64(i.ioapic_pa, 0xFEC00000u);
    CHECK(i.ioapic_id == 7 && i.sci_irq == 9 && i.smi_cmd == 0xB2 && i.acpi_enable == 0xF1);
    CHECK(i.pm1a_cnt == 0x604 && i.pm1b_cnt == 0);
    CHECK(i.has_s5 && i.slp_typa == 0 && i.slp_typb == 0);
    CHECK(i.ioapic_gsi_base == 0);                       /* W27-S01: overrides */
    CHECK(i.isa_gsi[0] == 2 && i.isa_flags[0] == 0);
    CHECK(i.isa_gsi[9] == 9 && i.isa_flags[9] == 0x0D);
    CHECK(i.isa_gsi[4] == 4 && i.isa_flags[4] == 0);    /* bus 1's override ignored */
    CHECK(i.isa_gsi[1] == 1 && i.isa_gsi[15] == 15);
}

/* ACPI 2.0: the RSDP in the EBDA, an XSDT preferred over the RSDT, a
 * 64-bit DSDT address, an APIC address override, and
 * Name(\_S5, Package(2){0x05, 0x07}) with byte prefixes. */
TEST(acpi_xsdt_ebda_override) {
    fresh();
    static const uint8_t aml[] = {0x08, '\\', '_', 'S', '5', '_', 0x12, 0x07, 0x02, 0x0A, 0x05, 0x0A, 0x07};
    dsdt(0x110000, aml, sizeof aml);
    fadt(0x111000, 0x1F0000 /* ignored: X_DSDT wins */, 0x110000);
    madt(0x112000, 1);
    header(0x113000, "XSDT", 52);
    put64(0x113000 + 36, 0x111000);
    put64(0x113000 + 44, 0x112000);
    sum_fix(0x113000, 52, 9);
    header(0x114000, "RSDT", 36);         /* an empty RSDT: must not be used */
    sum_fix(0x114000, 36, 9);
    put16(0x40E, 0x9FC0);                 /* the EBDA at 0x9FC00 */
    rsdp(0x9FC00 + 0x40, 2, 0x114000, 0x113000);
    struct acpi_info i;
    CHECK(acpi_parse(&g_m, &i) == ACPI_OK);
    CHECK(i.revision == 2 && i.ncpus == 4);
    CHECK_EQ_U64(i.lapic_pa, 0xFEE10000ull);
    CHECK(i.has_s5 && i.slp_typa == 5 && i.slp_typb == 7);
}

TEST(acpi_bad_checksums) {
    fresh();
    rsdp(0xE0000, 0, 0x103000, 0);
    g_mem[0xE0000 + 8] ^= 1;              /* a broken RSDP is not one */
    struct acpi_info i;
    CHECK(acpi_parse(&g_m, &i) == ACPI_NO_RSDP);
    fresh();
    fadt(0x101000, 0, 0);
    madt(0x102000, 0);
    g_mem[0x102000 + 50] ^= 1;            /* the MADT's checksum breaks: skipped */
    header(0x103000, "RSDT", 44);
    put32(0x103000 + 36, 0x101000);
    put32(0x103000 + 40, 0x102000);
    sum_fix(0x103000, 44, 9);
    rsdp(0xE0010, 0, 0x103000, 0);
    CHECK(acpi_parse(&g_m, &i) == ACPI_NO_MADT);
    CHECK(i.pm1a_cnt == 0x604);           /* the FADT was still read */
    CHECK(!i.has_s5);                     /* no DSDT */
}

TEST(acpi_error_names) {
    CHECK(strcmp(acpi_error(ACPI_NO_RSDP), "no RSDP") == 0);
    CHECK(strcmp(acpi_error(ACPI_OK), "ok") == 0);
}

/* W26-S03: UEFI firmware names the RSDP (boot_info's rsdp_addr), anywhere
 * in memory, not in the areas a BIOS scan reads; a wrong address falls
 * back to the scan. */
TEST(acpi_rsdp_from_firmware) {
    fresh();
    static const uint8_t aml[] = {0x08, '_', 'S', '5', '_', 0x12, 0x06, 0x04, 0x00, 0x00, 0x00, 0x00};
    dsdt(0x110000, aml, sizeof aml);
    fadt(0x111000, 0, 0x110000);
    madt(0x112000, 0);
    header(0x113000, "XSDT", 52);
    put64(0x113000 + 36, 0x111000);
    put64(0x113000 + 44, 0x112000);
    sum_fix(0x113000, 52, 9);
    rsdp(0x17E014, 2, 0, 0x113000);      /* OVMF's was at 0x7b7e014: not 16-aligned */
    struct acpi_info i;
    CHECK(acpi_parse(&g_m, &i) == ACPI_NO_RSDP);
    struct acpi_mem m = g_m;
    m.rsdp_pa = 0x17E014;
    CHECK(acpi_parse(&m, &i) == ACPI_OK);
    CHECK(i.revision == 2 && i.ncpus == 4);
    m.rsdp_pa = 0x17E000;                /* not an RSDP: the scan finds the BIOS one */
    rsdp(0xF5A10, 2, 0, 0x113000);
    CHECK(acpi_parse(&m, &i) == ACPI_OK);
}

/* W28-S04: a FADT with flags and a reset register (ACPI 2.0+, 244 bytes). */
static void fadt_reset(uint64_t pa, uint32_t dsdt, uint32_t flags, uint8_t space, uint64_t addr, uint8_t value) {
    header(pa, "FACP", 244);
    put32(pa + 40, dsdt);
    put32(pa + 64, 0x604);
    put32(pa + 112, flags);
    g_mem[pa + 116] = space;
    g_mem[pa + 117] = 8;                  /* a byte wide */
    put64(pa + 120, addr);
    g_mem[pa + 128] = value;
    sum_fix(pa, 244, 9);
}

static void ssdt(uint64_t pa, const uint8_t *aml, uint32_t n) {
    memcpy(g_mem + pa + 36, aml, n);
    header(pa, "SSDT", 36 + n);
    sum_fix(pa, 36 + n, 9);
}

/* rsdt_of lists n tables in an RSDT at 0x130000 and puts the RSDP in the BIOS area. */
static void rsdt_of(const uint32_t *t, uint32_t n) {
    header(0x130000, "RSDT", 36 + 4 * n);
    for (uint32_t i = 0; i < n; i++) put32(0x130000 + 36 + 4 * i, t[i]);
    sum_fix(0x130000, 36 + 4 * n, 9);
    rsdp(0xF0000, 0, 0x130000, 0);
}

/* A DSDT with no \_S5 and an SSDT that has it, as some firmware splits
 * them; the SSDT is listed before the FADT. A real machine's reset
 * register in I/O space (0xCF9 = 6). */
TEST(acpi_s5_in_ssdt_and_reset_register) {
    fresh();
    static const uint8_t none[] = {0x10, 0x05, 0x5C, '_', 'S', 'B', '_'};
    static const uint8_t s5[] = {0x08, '_', 'S', '5', '_', 0x12, 0x08, 0x04, 0x0A, 0x07, 0x0A, 0x07, 0x00, 0x00};
    dsdt(0x120000, none, sizeof none);
    ssdt(0x121000, s5, sizeof s5);
    fadt_reset(0x122000, 0x120000, ACPI_FADT_RESET_REG_SUP, ACPI_SPACE_IO, 0xCF9, 6);
    madt(0x123000, 0);
    const uint32_t t[] = {0x121000, 0x122000, 0x123000};
    rsdt_of(t, 3);
    struct acpi_info i;
    CHECK(acpi_parse(&g_m, &i) == ACPI_OK);
    CHECK(i.has_s5 && i.s5_table == ACPI_S5_SSDT && i.slp_typa == 7 && i.slp_typb == 7);
    CHECK(i.has_reset && i.reset_space == ACPI_SPACE_IO && i.reset_value == 6);
    CHECK_EQ_U64(i.reset_addr, 0xCF9);
}

/* Integer encodings: WordPrefix and Ones in a two-element package; a
 * one-element package (One) gives both types; a DWordPrefix'd package. */
TEST(acpi_s5_encodings) {
    static const uint8_t word_ones[] = {0x08, '_', 'S', '5', '_', 0x12, 0x07, 0x02, 0x0B, 0x05, 0x00, 0xFF};
    static const uint8_t one[] = {0x08, '_', 'S', '5', '_', 0x12, 0x03, 0x01, 0x01};
    static const uint8_t dword[] = {0x08, '_', 'S', '5', '_', 0x12, 0x0D, 0x02, 0x0C, 0x06, 0, 0, 0, 0x0C, 0x02, 0, 0, 0};
    static const struct { const uint8_t *aml; uint32_t n; uint16_t a, b; } c[] = {
        {word_ones, sizeof word_ones, 5, 7}, {one, sizeof one, 1, 1}, {dword, sizeof dword, 6, 2},
    };
    for (unsigned k = 0; k < 3; k++) {
        fresh();
        dsdt(0x120000, c[k].aml, c[k].n);
        fadt(0x122000, 0x120000, 0);
        madt(0x123000, 0);
        const uint32_t t[] = {0x122000, 0x123000};
        rsdt_of(t, 2);
        struct acpi_info i;
        CHECK(acpi_parse(&g_m, &i) == ACPI_OK);
        CHECK(i.has_s5 && i.s5_table == ACPI_S5_DSDT);
        CHECK(i.slp_typa == c[k].a && i.slp_typb == c[k].b);
        CHECK(!i.has_reset);                  /* a 116-byte FADT has none */
    }
}

/* The reset register only counts with RESET_REG_SUP, and in a space the
 * nucleus can write: memory and PCI configuration space are kept. */
TEST(acpi_reset_register_needs_its_flag) {
    fresh();
    fadt_reset(0x122000, 0, 0, ACPI_SPACE_IO, 0xCF9, 6);
    madt(0x123000, 0);
    const uint32_t t[] = {0x122000, 0x123000};
    rsdt_of(t, 2);
    struct acpi_info i;
    CHECK(acpi_parse(&g_m, &i) == ACPI_OK && !i.has_reset && !i.has_s5);
    fadt_reset(0x122000, 0, ACPI_FADT_RESET_REG_SUP, ACPI_SPACE_PCI, 0x0000001F00000044ull, 0xA);
    CHECK(acpi_parse(&g_m, &i) == ACPI_OK && i.has_reset && i.reset_space == ACPI_SPACE_PCI);
    CHECK_EQ_U64(i.reset_addr, 0x0000001F00000044ull);
    fadt_reset(0x122000, 0, ACPI_FADT_RESET_REG_SUP, 0x7F /* functional fixed hardware */, 0x1000, 1);
    CHECK(acpi_parse(&g_m, &i) == ACPI_OK && !i.has_reset);
}
