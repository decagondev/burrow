# ADR-0013: A UEFI loader of our own beside the BIOS chain

- **Status:** Accepted
- **Date:** 2026-11-13
- **Wave:** W26 (S01 the application and the disk, S02 the machine from the firmware, S03 the shared hand-off, S04 GOP, S05 the demos and a USB stick)

## Context
Burrow booted only from a BIOS: stage1 in the MBR, stage2 in real mode (E820,
VBE, long mode with 1 GiB identity-mapped), then the C loader reading the
manifest's payloads through its own ATA driver ([ADR-0002](ADR-0002-own-bios-bootloader.md),
[ADR-0008](ADR-0008-disk-manifest.md)). Most PCs sold in the last decade boot
UEFI only, or hide the BIOS path behind a setting, so RISKS.md R-06 had said
"UEFI path later" since W0. ADR-0002 left the door open: everything above the
loader depends only on `struct boot_info`, so a UEFI front end could fill the
same struct.

What had to stay true: the nucleus and the Go kernel do not care which
firmware started them; every payload is CRC-checked before it runs (BUG-0003);
the image is made by our own tools, reproducibly; and the BIOS images keep
booting with the markers they have.

## Options
1. **GRUB or Limine (Multiboot2, the Limine protocol)** - mature and boot on
   real hardware today, but a second boot protocol for the nucleus to accept,
   a large dependency on the image, and the part of booting this project
   exists to show would be someone else's.
2. **gnu-efi or EDK II for a UEFI application** - the usual toolkits, with the
   firmware's types and a PE linker script; each is a build dependency larger
   than the loader, and EDK II brings its own build system.
3. **A UEFI application of our own** - freestanding C with the few protocol
   structures it uses declared in one header, linked by the toolchain we have,
   turned into a PE32+ by `mkimage`; it fills `boot_info` from the firmware
   and calls the BIOS loader's back half.
4. **Make the firmware's machine the nucleus's problem** - pass the UEFI
   memory map and GOP's mode through as they are, and teach the nucleus both
   shapes; two formats in every consumer, for no gain.

## Decision
Option 3, with one `boot_info` for both paths.

- **Two front halves, one back half.** `loader/efi/` (`efi.h`, `main.c`,
  `firmware.c`, `files.c`, `gop.c`) is the UEFI front half; stage1 and stage2
  stay the BIOS one. What the BIOS loader did from the nucleus's ELF onwards
  (segments into 1-4 MiB, boot modules, page tables, the jump) is
  `loader/handoff.c`, which both loaders call.
- **The firmware describes the machine in the BIOS path's terms.**
  `GetMemoryMap` becomes E820 records (`efimap.c`, pure), the RSDP from the
  configuration table goes into a new field (`boot_info` v4, `rsdp_addr`; 0 on
  the BIOS path), GOP's mode into the framebuffer fields with `fb_source` 2.
- **The fixed addresses stay fixed.** The loader claims 0x1000-0x2FFF, 0x8000
  and 1-4 MiB with `AllocatePages(AllocateAddress)` and stops, naming the
  range, if the firmware owns one; OVMF owns none, so nothing was made
  movable.
- **The disk is GPT with an EFI system partition.** `mkimage -esp-dir` writes
  the partition's files (`\EFI\BOOT\BOOTX64.EFI`, `burrow/manifest` with the
  BIOS layout's CRCs, each payload under `burrow/`), `kernel/cmd/mkfat` the
  FAT32 volume `BURROWBOOT`, `mkimage -gpt` the disk, with data volumes as
  further partitions (`-part`). `binutils`' PE linker writes no base
  relocations for ELF objects, so the loader is linked as a static PIE and
  `mkimage -efi-app` writes the PE32+ and its relocations itself.
- **Leaving the firmware** is `ExitBootServices` with the key of a map read
  into a buffer taken beforehand and converted without printing, one retry,
  then `cli` and the shared jump. No runtime services are kept: the clock is
  the CMOS's, power-off is ACPI's.

## Consequences
- The nucleus changed in two places only: it takes the RSDP from `boot_info`
  when it is set, and it draws its lines on GOP's framebuffer until the Go
  kernel starts (`nucleus/screen.c`), because a real PC rarely has a serial
  port. OVMF's machine state needed nothing (BOOT_PROTOCOL.md §8).
- The Go kernel knows a GPT boot disk: it names the boot partition, never
  mounts it, and mounts the disk's other partitions by label as `hda2`..
  (`kernel/block/gpt`, `bootparts.go`), so one USB stick carries the root and
  the data volumes.
- Every Go image has a UEFI twin from the same payload list, and `UEFI=1`
  makes the run targets boot them; `make uefi-smoke`, `uefi-ktest` and
  `uefi-interop` keep both paths in `make test`.
- Boots got faster under UEFI on QEMU: 7.3 s from the firmware's first line
  to `BURROW:GO:OK` against 15.4 s, because OVMF's block I/O reads the
  payloads about four times as fast as the BIOS loader's ATA PIO (`make
  boot-bench`). The Go kernel's segments map 1.1 s slower after OVMF than after
  SeaBIOS, because TCG makes the first stores into frames that held
  OVMF's code expensive (BUG-0036, QEMU's cost, not Burrow's).
- Not decided here and not done: Secure Boot (the loader is unsigned),
  runtime services, a boot menu, El Torito, a 32-bit UEFI. Real PCs need
  drivers QEMU's `pc` machine does not (W27).
- Tests: `TestELFToPE`, `TestBuildGPT`, `TestBuildGPTDataPartitions`,
  `test_efimap.c`, `test_gopmode.c`, `TestCFontMatchesGo`, `kt_screen_gop_text`,
  `TestPartitionWriterConformance`, REG-0010 and REG-0011, and the UEFI smokes.
