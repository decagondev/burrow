/* tlb.h - TLB shootdown (W20-S07, ADR-0011). After the nucleus changes or
 * removes page-table entries of an address space, every CPU that may cache
 * them must flush before the change counts: before a frame is reused, before
 * a write-protected page is trusted to fault. A CPU may cache entries of the
 * root its CR3 holds (percpu.cr3) and, whatever its CR3, of the shared upper
 * half. tlb_shootdown flushes here, sends VEC_SHOOT to each other such CPU
 * and waits for each to answer. A CPU waiting with interrupts off (in a
 * spinlock, or in tlb_shootdown itself) answers through tlb_poll, so two
 * CPUs can never wait for each other. */
#ifndef NUCLEUS_TLB_H
#define NUCLEUS_TLB_H
#include <stdint.h>

#define VEC_SHOOT        0xF1
#define TLB_FULL_PAGES   32u   /* more pages than this: reload CR3 instead of invlpg each */

void tlb_init(void);           /* the VEC_SHOOT handler (before the APs start) */
/* Flushes [va, va+len) of the space whose PML4 is at root on every CPU that
 * may cache it, this one included. Any context but an IRQ handler; IF either way. */
void tlb_shootdown(uint64_t root, uint64_t va, uint64_t len);
/* Answers this CPU's pending shootdown, if any (IF=0, from wait loops). */
void tlb_poll(void);
/* The scheduler tells which root this CPU now runs on (after its mov to CR3). */
void tlb_set_loaded(uint64_t root);
#endif
