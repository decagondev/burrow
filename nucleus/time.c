/* time.c - see time.h. PIT: 1.193182 MHz input clock; channel 2's gate and
 * output are wired to port 0x61 bits 0 and 5, so it can be polled without
 * an interrupt (the classic Linux pit_calibrate_tsc trick). */
#include "vdso.h"
#include "time.h"
#include "apic.h"
#include "trap.h"
#include "percpu.h"
#include "io.h"
#include "cpu.h"
#include "kprintf.h"
#include "panic.h"

#define PIT_HZ          1193182ull
#define PIT_CH2         0x42
#define PIT_CMD         0x43
#define PIT_GATE_PORT   0x61
#define CALIBRATE_MS    50ull

#define LVT_MASKED      (1u << 16)
#define LVT_PERIODIC    (1u << 17)
#define TIMER_DIV_16    0x3u

static uint64_t g_tsc_hz, g_tsc0, g_ns_mult;   /* ns = (tsc_delta * g_ns_mult) >> 32 */
static volatile uint64_t g_ticks;
static volatile uint64_t g_realtime_base_ns;   /* wall time at monotonic 0 */
static void (*g_tick_hook)(void);
static uint32_t g_lapic_per_ms;               /* calibrated once, on the BSP */

/* Count TSC cycles while PIT channel 2 counts down CALIBRATE_MS once. */
static uint64_t calibrate_tsc(void) {
    uint16_t count = (uint16_t)(PIT_HZ * CALIBRATE_MS / 1000);
    outb(PIT_GATE_PORT, (uint8_t)((inb(PIT_GATE_PORT) & ~0x02) | 0x01));   /* gate on, speaker off */
    outb(PIT_CMD, 0xB0);                         /* channel 2, lo/hi byte, mode 0 */
    outb(PIT_CH2, (uint8_t)count);
    outb(PIT_CH2, (uint8_t)(count >> 8));
    uint64_t t0 = rdtsc();
    while (!(inb(PIT_GATE_PORT) & 0x20)) { }     /* OUT2 goes high at terminal count */
    return (rdtsc() - t0) * 1000 / CALIBRATE_MS;
}

static void on_tick(struct trap_frame *f) {
    (void)f;
    if (this_cpu()->index == 0) g_ticks++;   /* one clock: the BSP's */
    lapic_eoi();
    if (g_tick_hook) g_tick_hook();
}

/* LAPIC timer counts per millisecond, measured against the TSC. */
static uint32_t calibrate_lapic(void) {
    lapic_write(LAPIC_TIMER_DIV, TIMER_DIV_16);
    lapic_write(LAPIC_LVT_TIMER, LVT_MASKED | VEC_TIMER);       /* one-shot, masked */
    lapic_write(LAPIC_TIMER_INIT, 0xFFFFFFFFu);
    time_busy_wait_ns(10ull * 1000 * 1000);
    uint32_t elapsed = 0xFFFFFFFFu - lapic_read(LAPIC_TIMER_COUNT);
    lapic_write(LAPIC_TIMER_INIT, 0);
    return elapsed / 10;
}

void time_init(void) {
    g_tsc_hz = calibrate_tsc();
    if (g_tsc_hz < 1000000) panic("TSC calibration failed (%llu Hz)", (unsigned long long)g_tsc_hz);
    g_ns_mult = (1000000000ull << 32) / g_tsc_hz;
    g_tsc0 = rdtsc();
    vdso_update(g_tsc0, g_ns_mult, g_realtime_base_ns);   /* W25-S02: the clock for every process */

    uint32_t per_ms = calibrate_lapic();
    if (per_ms == 0) panic("LAPIC timer calibration failed");
    g_lapic_per_ms = per_ms;
    trap_set_handler(VEC_TIMER, on_tick);
    time_init_cpu();
    kprintf("[nucleus] time ok (tsc %llu MHz, lapic timer %u counts/ms, %u Hz tick)\n",
            (unsigned long long)(g_tsc_hz / 1000000), per_ms, TIMER_HZ);
}

/* This CPU's LAPIC timer at TIMER_HZ (the BSP's calibration: one bus clock). */
void time_init_cpu(void) {
    lapic_write(LAPIC_TIMER_DIV, TIMER_DIV_16);
    lapic_write(LAPIC_LVT_TIMER, LVT_PERIODIC | VEC_TIMER);
    lapic_write(LAPIC_TIMER_INIT, g_lapic_per_ms * (1000 / TIMER_HZ));
}

uint64_t time_monotonic_ns(void) {
    unsigned __int128 p = (unsigned __int128)(rdtsc() - g_tsc0) * g_ns_mult;   /* mul only: no libgcc */
    return (uint64_t)(p >> 32);
}

void time_set_realtime_ns(uint64_t unix_ns) {
    g_realtime_base_ns = unix_ns - time_monotonic_ns();
    vdso_update(g_tsc0, g_ns_mult, g_realtime_base_ns);
}
uint64_t time_realtime_ns(void) { return g_realtime_base_ns + time_monotonic_ns(); }

void time_busy_wait_ns(uint64_t ns) {
    uint64_t end = time_monotonic_ns() + ns;
    while (time_monotonic_ns() < end) __asm__ volatile("pause");
}

uint64_t time_ticks(void)  { return g_ticks; }
uint64_t time_tsc_hz(void) { return g_tsc_hz; }
uint64_t time_ticks_to_ns(uint64_t tsc_ticks) {   /* W25-S01: a TSC interval in ns */
    unsigned __int128 p = (unsigned __int128)tsc_ticks * g_ns_mult;
    return (uint64_t)(p >> 32);
}
void time_set_tick_hook(void (*hook)(void)) { g_tick_hook = hook; }
