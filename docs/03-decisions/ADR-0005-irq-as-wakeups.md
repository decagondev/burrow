# ADR-0005: Interrupts become goroutine wake-ups

- **Status:** Accepted
- **Date:** 2026-11-02
- **Wave:** W7

## Context
Go code can allocate, grow its stack, or hit a GC barrier at nearly any call. None of that is safe inside an interrupt handler.

## Decision
The nucleus owns the IDT and all ISRs. An ISR masks the line, sends EOI, and wakes the nucleus thread blocked in `nu_irq_wait(n)`. A driver goroutine (locked to that thread) then runs ordinary Go code and calls `nu_irq_ack(n)`. Same idea as TamaGo's `runtime.WakeG`, without patching the runtime.

## Consequences
- Interrupt latency includes a thread wake-up (microseconds): fine for keyboard, disk, timer-driven work; revisit for high-rate NICs (batching).
- One parked OS thread per IRQ line in use.
- Drivers are plain Go, unit-testable with a fake `IRQLine`.
