/* time.h - clocks. The TSC (calibrated once against PIT channel 2) is the
 * time source; the LAPIC timer (calibrated against the TSC) ticks at
 * TIMER_HZ and drives preemption (sched.c). Assumes an invariant TSC, which
 * QEMU provides; real hardware without one is out of scope for now. */
#ifndef NUCLEUS_TIME_H
#define NUCLEUS_TIME_H
#include <stdint.h>

#define TIMER_HZ      1000u
#define VEC_TIMER     0x30

void     time_init(void);              /* after lapic_init; leaves IF unchanged */
uint64_t time_monotonic_ns(void);      /* since time_init */
/* Wall time (W14-S04): Go reads the CMOS clock and sets it once; until then
 * it is time since boot, as before. */
void     time_set_realtime_ns(uint64_t unix_ns);
uint64_t time_realtime_ns(void);
uint64_t time_ticks(void);             /* LAPIC timer interrupts so far */
uint64_t time_tsc_hz(void);
void     time_busy_wait_ns(uint64_t ns);
/* Called on every timer tick, in IRQ context, after EOI (sched.c hooks in). */
void     time_set_tick_hook(void (*hook)(void));
#endif
