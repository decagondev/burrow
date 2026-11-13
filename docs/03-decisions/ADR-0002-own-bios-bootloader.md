# ADR-0002: Own BIOS bootloader first, UEFI later

- **Status:** Accepted
- **Date:** 2026-11-02
- **Wave:** W1

## Context
Options for getting into long mode: write our own legacy-BIOS loader, use GRUB (Multiboot2), use Limine, or write a UEFI application.

## Options
1. **GRUB/Limine** — robust, fast start, handles framebuffer/memory map. Hides how booting works.
2. **UEFI app** — modern, real hardware friendly; needs PE/COFF toolchain and firmware APIs, less "from scratch" feel.
3. **Own MBR + stage2 (BIOS)** — maximum learning, runs on QEMU's SeaBIOS and most PCs with CSM.

## Decision
Option 3 now. The loader and everything above it only depend on `struct boot_info`, so a UEFI front-end (E12) can later produce the same struct (Open/Closed).

## Consequences
- 16-bit real-mode code to learn and debug (see DEBUGGING.md).
- Machines without CSM need the UEFI path later: W26 added it beside this one ([ADR-0013](ADR-0013-uefi-loader.md)).
- Limine/GRUB stay available as a debugging fallback: if our boot breaks, boot the nucleus via Multiboot2 to isolate the problem (optional slice).
