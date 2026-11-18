/* acpi_boot.c - ACPI on the machine (W20-S04): acpi_init reads the tables
 * once at boot through the physmap (acpi.c does the reading; a probe keeps
 * an address past the physmap from faulting) and prints the CPUs; acpi_info
 * hands them to the SMP bring-up (S05); acpi_poweroff turns the machine off
 * by writing \_S5's sleep type with SLP_EN to the PM1 control ports,
 * switching ACPI mode on first through the SMI command port if the
 * firmware left it off; acpi_reset (W28-S04) restarts it through the FADT's
 * reset register, then port 0xCF9, then the keyboard controller, then a
 * triple fault, saying on the console and the screen what each step did
 * before the machine goes away (PIT-109). acpi_poweroff never writes a
 * port the FADT did not name. */
#include "acpi.h"
#include "boot.h"
#include "io.h"
#include "kprintf.h"
#include "trap.h"
#include "vmm.h"
#include "cpu.h"

#define RESET_SETTLE_POLLS 300000u   /* io_wait()s, about 0.3 s, before the next way is tried */
#define PORT_RESET_CTL     0xCF9u    /* the chipset's reset control */
#define RESET_CTL_SYS      0x02u     /* system reset, armed */
#define RESET_CTL_HARD     0x06u     /* ... and done, a full reset */
#define PORT_KBC_CMD       0x64u
#define KBC_PULSE_RESET    0xFEu
#define PCI_CONFIG_ADDR    0xCF8u
#define PCI_CONFIG_DATA    0xCFCu

static struct acpi_info g_acpi;
static int g_acpi_ok;

static const char *space_name(uint8_t space) {
    return space == ACPI_SPACE_IO ? "io" : space == ACPI_SPACE_MEMORY ? "memory" : "pci";
}

static void settle(void) {
    for (uint32_t i = 0; i < RESET_SETTLE_POLLS; i++) io_wait();
}

/* reset_register writes the FADT's reset value to its register: an I/O
 * port, a byte of device memory, or a byte of bus 0's configuration space
 * (device in address bits 47:32, function in 31:16, offset in 15:0). */
static void reset_register(void) {
    uint64_t a = g_acpi.reset_addr;
    switch (g_acpi.reset_space) {
    case ACPI_SPACE_IO:
        outb((uint16_t)a, g_acpi.reset_value);
        break;
    case ACPI_SPACE_MEMORY: {
        void *va;
        if (vm_map_mmio_type(a & ~0xFFFull, 0x1000, PTE_PCD | PTE_PWT, &va) == 0)
            *((volatile uint8_t *)va + (a & 0xFFF)) = g_acpi.reset_value;
        break;
    }
    case ACPI_SPACE_PCI: {
        uint32_t dev = (uint32_t)(a >> 32) & 0x1F, fn = (uint32_t)(a >> 16) & 7, off = (uint32_t)a & 0xFF;
        outl(PCI_CONFIG_ADDR, 0x80000000u | dev << 11 | fn << 8 | (off & 0xFC));
        outb((uint16_t)(PCI_CONFIG_DATA + (off & 3)), g_acpi.reset_value);
        break;
    }
    }
}

void acpi_reset(uint32_t from) {
    cpu_cli();
    if (from <= ACPI_RESET_REGISTER && g_acpi_ok && g_acpi.has_reset) {
        kprintf("[nucleus] reset: the FADT's reset register (%s %llx = %x)\n", space_name(g_acpi.reset_space),
                (unsigned long long)g_acpi.reset_addr, g_acpi.reset_value);
        reset_register();
        settle();
    }
    if (from <= ACPI_RESET_CF9) {
        kprintf("[nucleus] reset: port cf9\n");
        outb(PORT_RESET_CTL, RESET_CTL_SYS);
        io_wait();
        outb(PORT_RESET_CTL, RESET_CTL_HARD);
        settle();
    }
    if (from <= ACPI_RESET_KBC) {
        kprintf("[nucleus] reset: keyboard controller\n");
        outb(PORT_KBC_CMD, KBC_PULSE_RESET);
        settle();
    }
    kprintf("[nucleus] reset: triple fault\n");
    cpu_triple_fault();
}

static const void *physmap(void *ctx, uint64_t pa, uint64_t len) {
    (void)ctx;
    uint64_t v;
    if (len == 0 || probe_read64((uint64_t)(uintptr_t)phys_to_virt(pa & ~7ull), &v) != 0 ||
        probe_read64((uint64_t)(uintptr_t)phys_to_virt((pa + len - 1) & ~7ull), &v) != 0)
        return 0;   /* not in the physmap */
    return phys_to_virt(pa);
}

void acpi_init(void) {
    const struct acpi_mem m = {physmap, 0, boot_info()->rsdp_addr};
    if (m.rsdp_pa) kprintf("[nucleus] acpi: rsdp from boot_info at %llx\n", (unsigned long long)m.rsdp_pa);
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
    if (g_acpi.has_s5 && g_acpi.s5_table == ACPI_S5_SSDT) kprintf("[nucleus] acpi: S5 from an SSDT\n");
    if (g_acpi.has_reset)
        kprintf("[nucleus] acpi: reset register %s %llx = %x\n", space_name(g_acpi.reset_space),
                (unsigned long long)g_acpi.reset_addr, g_acpi.reset_value);
    else
        kprintf("[nucleus] acpi: no reset register\n");
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
