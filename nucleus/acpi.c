/* acpi.c - see acpi.h (W20-S04). Pure: physical memory only through
 * acpi_mem, no I/O. Finds the RSDP on a 16-byte boundary in the EBDA's
 * first KiB or in 0xE0000-0xFFFFF, takes the XSDT when the RSDP is ACPI
 * 2.0+ and has one, else the RSDT, and reads the MADT (processors with the
 * enabled flag, the first I/O APIC, an APIC address override), the FADT
 * (SCI, SMI command, PM1 control blocks, the DSDT, X_DSDT when there) and
 * the DSDT's \_S5 package, found by the usual scan for its AML (NameOp
 * "_S5_" PackageOp) rather than by an AML interpreter. Never trusts a
 * length or a pointer before mapping it, and skips tables whose checksum
 * is wrong. */
#include "acpi.h"
#include "lib_string.h"

#define EBDA_SEG_PA  0x40Eu
#define BIOS_LO      0xE0000u
#define BIOS_HI      0x100000u
#define HDR_BYTES    36u
#define AML_NAME     0x08
#define AML_PACKAGE  0x12
#define AML_BYTE     0x0A

static uint32_t rd32(const uint8_t *p) { uint32_t v; memcpy(&v, p, 4); return v; }
static uint64_t rd64(const uint8_t *p) { uint64_t v; memcpy(&v, p, 8); return v; }
static uint16_t rd16(const uint8_t *p) { uint16_t v; memcpy(&v, p, 2); return v; }

static int sums_to_zero(const uint8_t *p, uint32_t n) {
    uint8_t s = 0;
    for (uint32_t i = 0; i < n; i++) s += p[i];
    return s == 0;
}

/* table maps a whole table at pa whose signature is sig (0: any), checksum checked. */
static const uint8_t *table(const struct acpi_mem *m, uint64_t pa, const char *sig, uint32_t *len) {
    const uint8_t *h = m->map(m->ctx, pa, HDR_BYTES);
    if (!h || (sig && memcmp(h, sig, 4) != 0)) return 0;
    uint32_t n = rd32(h + 4);
    if (n < HDR_BYTES || n > (1u << 24)) return 0;
    const uint8_t *t = m->map(m->ctx, pa, n);
    if (!t || !sums_to_zero(t, n)) return 0;
    *len = n;
    return t;
}

static const uint8_t *rsdp_in(const struct acpi_mem *m, uint64_t lo, uint64_t hi) {
    for (uint64_t pa = lo; pa + 20 <= hi; pa += 16) {
        const uint8_t *p = m->map(m->ctx, pa, 20);
        if (p && memcmp(p, "RSD PTR ", 8) == 0 && sums_to_zero(p, 20)) {
            if (p[15] >= 2) {
                const uint8_t *x = m->map(m->ctx, pa, 36);
                if (!x || !sums_to_zero(x, 36)) continue;
                return x;
            }
            return p;
        }
    }
    return 0;
}

static const uint8_t *find_rsdp(const struct acpi_mem *m) {
    const uint8_t *seg = m->map(m->ctx, EBDA_SEG_PA, 2);
    if (seg) {
        uint64_t ebda = (uint64_t)rd16(seg) << 4;
        const uint8_t *r = ebda ? rsdp_in(m, ebda, ebda + 1024) : 0;
        if (r) return r;
    }
    return rsdp_in(m, BIOS_LO, BIOS_HI);
}

static void read_madt(const uint8_t *t, uint32_t len, struct acpi_info *out) {
    out->lapic_pa = rd32(t + 36);
    for (uint32_t off = 44; off + 2 <= len;) {
        uint8_t type = t[off], n = t[off + 1];
        if (n < 2 || off + n > len) break;
        if (type == 0 && n >= 8 && (rd32(t + off + 4) & 1) && out->ncpus < ACPI_MAX_CPUS)
            out->apic_id[out->ncpus++] = t[off + 3];
        else if (type == 1 && n >= 12 && !out->ioapic_pa) {
            out->ioapic_id = t[off + 2];
            out->ioapic_pa = rd32(t + off + 4);
        } else if (type == 5 && n >= 12)
            out->lapic_pa = rd64(t + off + 4);
        off += n;
    }
}

/* aml_int reads one package element within len: a BytePrefix'd byte, or
 * Zero/One/a small constant. */
static int aml_int(const uint8_t *p, uint32_t len, uint32_t *i) {   /* -1: past the end */
    if (*i >= len) return -1;
    if (p[*i] == AML_BYTE) {
        if (*i + 1 >= len) return -1;
        int v = p[*i + 1];
        *i += 2;
        return v;
    }
    return p[(*i)++];
}

static void read_s5(const uint8_t *d, uint32_t len, struct acpi_info *out) {
    for (uint32_t i = HDR_BYTES + 1; i + 7 <= len; i++) {   /* "_S5_", PackageOp, PkgLength, NumElements */
        if (memcmp(d + i, "_S5_", 4) != 0) continue;
        int named = d[i - 1] == AML_NAME || (d[i - 1] == '\\' && d[i - 2] == AML_NAME);
        if (!named || d[i + 4] != AML_PACKAGE) continue;
        uint32_t p = i + 5;
        p += ((d[p] >> 6) & 3) + 1;       /* PkgLength: 1 to 4 bytes */
        if (p >= len || d[p++] < 2) return;   /* NumElements */
        int a = aml_int(d, len, &p), b = aml_int(d, len, &p);
        if (a < 0 || b < 0) return;
        out->slp_typa = (uint16_t)a;
        out->slp_typb = (uint16_t)b;
        out->has_s5 = 1;
        return;
    }
}

static int read_fadt(const struct acpi_mem *m, const uint8_t *t, uint32_t len, struct acpi_info *out) {
    out->sci_irq = rd16(t + 46);
    out->smi_cmd = rd32(t + 48);
    out->acpi_enable = t[52];
    out->pm1a_cnt = (uint16_t)rd32(t + 64);
    out->pm1b_cnt = (uint16_t)rd32(t + 68);
    uint64_t dsdt = rd32(t + 40);
    if (len >= 148 && rd64(t + 140)) dsdt = rd64(t + 140);
    uint32_t dlen;
    const uint8_t *d = dsdt ? table(m, dsdt, "DSDT", &dlen) : 0;
    if (d) read_s5(d, dlen, out);
    return 1;
}

int acpi_parse(const struct acpi_mem *m, struct acpi_info *out) {
    memset(out, 0, sizeof *out);
    const uint8_t *r = find_rsdp(m);
    if (!r) return ACPI_NO_RSDP;
    out->revision = r[15];
    int wide = r[15] >= 2 && rd64(r + 24);
    uint32_t len;
    const uint8_t *sdt = wide ? table(m, rd64(r + 24), "XSDT", &len) : table(m, rd32(r + 16), "RSDT", &len);
    if (!sdt) return ACPI_NO_RSDP;
    int madt = 0, fadt = 0;
    uint32_t step = wide ? 8 : 4;
    for (uint32_t off = HDR_BYTES; off + step <= len; off += step) {
        uint64_t pa = wide ? rd64(sdt + off) : rd32(sdt + off);
        uint32_t tlen;
        const uint8_t *t = table(m, pa, 0, &tlen);
        if (!t) continue;
        if (!madt && memcmp(t, "APIC", 4) == 0) {
            read_madt(t, tlen, out);
            madt = 1;
        } else if (!fadt && memcmp(t, "FACP", 4) == 0 && tlen >= 72)
            fadt = read_fadt(m, t, tlen, out);
    }
    if (!madt) return ACPI_NO_MADT;
    return fadt ? ACPI_OK : ACPI_NO_FADT;
}

const char *acpi_error(int code) {
    switch (code) {
    case ACPI_OK: return "ok";
    case ACPI_NO_RSDP: return "no RSDP";
    case ACPI_NO_MADT: return "no MADT";
    case ACPI_NO_FADT: return "no FADT";
    }
    return "?";
}
