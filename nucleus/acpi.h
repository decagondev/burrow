/* acpi.h - the machine's ACPI tables (W20-S04), read once at boot: the
 * CPUs (local APIC ids from the MADT), the I/O APIC, and what turning the
 * machine off takes (the FADT's PM1 control ports, the DSDT's \_S5 sleep
 * type, the SMI command that enables ACPI mode). acpi.c is pure: it reads
 * physical memory only through acpi_mem (the nucleus's physmap, or a host
 * test's buffer), never I/O ports. ACPI 6.5 spec, ch. 5.2. */
#ifndef NUCLEUS_ACPI_H
#define NUCLEUS_ACPI_H
#include <stdint.h>

#define ACPI_MAX_CPUS 64u
#define ACPI_SLP_EN   (1u << 13)   /* PM1 control: sleep enable */
#define ACPI_SCI_EN   1u           /* PM1 control: ACPI mode on */

/* acpi_mem maps len bytes of physical memory at pa for reading; 0 if not there. */
struct acpi_mem {
    const void *(*map)(void *ctx, uint64_t pa, uint64_t len);
    void *ctx;
};

struct acpi_info {
    uint8_t  revision;                 /* the RSDP's: 0 (RSDT), 2+ (XSDT) */
    uint32_t ncpus;                    /* enabled processors, at most ACPI_MAX_CPUS */
    uint8_t  apic_id[ACPI_MAX_CPUS];   /* their local APIC ids, in MADT order */
    uint64_t lapic_pa;                 /* the local APICs' address (an override wins) */
    uint64_t ioapic_pa;                /* the first I/O APIC, 0 if none */
    uint8_t  ioapic_id;
    uint16_t sci_irq;
    uint32_t smi_cmd;                  /* port to send acpi_enable to, 0 if ACPI is always on */
    uint8_t  acpi_enable;
    uint16_t pm1a_cnt, pm1b_cnt;       /* PM1 control ports (b: 0 if none) */
    int      has_s5;                   /* \_S5 found in the DSDT */
    uint16_t slp_typa, slp_typb;       /* its sleep types for PM1a and PM1b */
};

enum { ACPI_OK = 0, ACPI_NO_RSDP = -1, ACPI_NO_MADT = -2, ACPI_NO_FADT = -3 };

/* acpi_parse finds the RSDP (the EBDA's first KiB, then 0xE0000-0xFFFFF),
 * walks the XSDT (or the RSDT), and reads the MADT, the FADT and the DSDT.
 * Tables with a bad checksum are skipped. */
int acpi_parse(const struct acpi_mem *m, struct acpi_info *out);
const char *acpi_error(int code);

/* acpi_boot.c (the nucleus, not host-tested): */
void acpi_init(void);                        /* after vmm_init: prints "[nucleus] acpi ..." */
const struct acpi_info *acpi_machine(void);  /* 0 when the tables could not be read */
void acpi_poweroff(void);                    /* returns only if ACPI cannot do it */
#endif
