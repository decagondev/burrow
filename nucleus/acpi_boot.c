/* acpi_boot.c - ACPI on the machine (W20-S04): acpi_init reads the tables
 * once at boot through the physmap (acpi.c does the reading; a probe keeps
 * an address past the physmap from faulting) and prints the CPUs; acpi_info
 * hands them to the SMP bring-up (S05); acpi_poweroff turns the machine off
 * by writing \_S5's sleep type with SLP_EN to the PM1 control ports,
 * switching ACPI mode on first through the SMI command port if the
 * firmware left it off. Never writes a port the FADT did not name. */
#include "acpi.h"
#include "boot.h"
#include "io.h"
#include "kprintf.h"
#include "trap.h"

static struct acpi_info g_acpi;
static int g_acpi_ok;

static const void *physmap(void *ctx, uint64_t pa, uint64_t len) {
    (void)ctx;
    uint64_t v;
    if (len == 0 || probe_read64((uint64_t)(uintptr_t)phys_to_virt(pa & ~7ull), &v) != 0 ||
        probe_read64((uint64_t)(uintptr_t)phys_to_virt((pa + len - 1) & ~7ull), &v) != 0)
        return 0;   /* not in the physmap */
    return phys_to_virt(pa);
}

void acpi_init(void) {
    const struct acpi_mem m = {physmap, 0};
    int rc = acpi_parse(&m, &g_acpi);
    g_acpi_ok = rc == ACPI_OK;
    if (!g_acpi_ok) {
        kprintf("[nucleus] acpi: %s: one CPU, no ACPI poweroff\n", acpi_error(rc));
        return;
    }
    kprintf("[nucleus] acpi ok (%u CPU%s, apic ids", g_acpi.ncpus, g_acpi.ncpus == 1 ? "" : "s");
    for (uint32_t i = 0; i < g_acpi.ncpus; i++) kprintf(" %u", g_acpi.apic_id[i]);
    kprintf("; I/O APIC %u at %llx; S5 %s on port %x)\n", g_acpi.ioapic_id, (unsigned long long)g_acpi.ioapic_pa,
            g_acpi.has_s5 ? "found" : "missing", g_acpi.pm1a_cnt);
}

const struct acpi_info *acpi_machine(void) { return g_acpi_ok ? &g_acpi : 0; }

void acpi_poweroff(void) {
    if (!g_acpi_ok || !g_acpi.has_s5 || !g_acpi.pm1a_cnt) return;
    if (g_acpi.smi_cmd && g_acpi.acpi_enable && !(inw(g_acpi.pm1a_cnt) & ACPI_SCI_EN)) {
        outb((uint16_t)g_acpi.smi_cmd, g_acpi.acpi_enable);
        for (int i = 0; i < 300000 && !(inw(g_acpi.pm1a_cnt) & ACPI_SCI_EN); i++) io_wait();
    }
    kprintf("[nucleus] acpi: powering off\n");
    outw(g_acpi.pm1a_cnt, (uint16_t)(g_acpi.slp_typa << 10) | ACPI_SLP_EN);
    if (g_acpi.pm1b_cnt) outw(g_acpi.pm1b_cnt, (uint16_t)(g_acpi.slp_typb << 10) | ACPI_SLP_EN);
}
