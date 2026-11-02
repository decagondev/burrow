# ADR-0007: Serial markers + QEMU debug-exit as the test interface

- **Status:** Accepted
- **Date:** 2026-11-02
- **Wave:** W0

## Context
Kernel code is hard to test. We need fast feedback at every layer without an LLM, an IDE, or a human watching a window.

## Decision
- Pure logic is extracted into modules that compile on the host and are unit-tested (C: minitest + ASan/UBSan; Go: `go test`).
- In-VM tests communicate through **ordered serial markers** and the **`isa-debug-exit` device** (`outb 0xF4, 0x10` = pass -> QEMU status 33; `0x11` = fail -> 35).
- Every reproducible bug gets a regression case named after its BUG id.

## Consequences
- Marker text is an API (append-only).
- The same image boots interactively (`make run`) and in tests (`make smoke`); port 0xF4 is a no-op on real hardware.
