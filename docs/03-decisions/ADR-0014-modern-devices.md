# ADR-0014: The I/O APIC and MSI beside virtual wire; one USB stack for HID and storage

- **Status:** Accepted
- **Date:** 2026-11-14
- **Wave:** W27 (S01 interrupts, S02 AHCI and NVMe, S03 xHCI and HID, S04 USB storage, S05 the stick and the hardware report)

## Context
Through W26 Burrow drove QEMU's `pc` machine: interrupts through the 8259 in
virtual-wire mode ([ADR-0005](ADR-0005-irq-as-wakeups.md): an interrupt is a
wake-up for one goroutine, the line masked until its driver acks), disks on IDE
or virtio, a PS/2 keyboard and mouse. A PC of the last decade has none of the
last two, and its devices interrupt through the I/O APIC or by message (MSI,
MSI-X); a USB stick Burrow boots from is itself a USB disk. ADR-0013 made the
firmware path work; without these devices a real PC reached the Go console and
could do nothing there (RISKS.md R-06).

What had to stay true: ADR-0005's model (Go never runs in an interrupt; the
nucleus masks, acknowledges and wakes); every `pc` smoke as it was; drivers that
touch hardware only through `driver.Env`, host-tested against fakes; nothing a
tester cannot report from the screen.

## Options
1. **Interrupts: keep virtual wire, add MSI only** - the 8259 still routes the
   ISA lines on most chipsets, but not the level-triggered PCI lines past 15,
   and a machine without a working 8259 gets nothing.
2. **Interrupts: the I/O APIC from the MADT for the ISA and INTx lines, MSI and
   MSI-X for PCI devices that have them, virtual wire kept as a fallback**
   (the MADT has no I/O APIC, or kargs `irq=pic`).
3. **Interrupts: the I/O APIC with PCI routing from ACPI's `_PRT`** - right for
   INTx behind bridges, but it needs an AML interpreter.
4. **USB: a driver per class, each talking to the controller** - quick for one
   keyboard, but storage, hubs and a second controller type would each repeat
   enumeration.
5. **USB: one stack** - a host controller driver that enumerates devices and
   serves control, interrupt and bulk transfers (`drivers/xhci`), and a
   controller-free layer above it (`kernel/usb`) with descriptors and the
   class drivers.

## Decision
Options 2 and 5.

- **Lines are what they were.** ISA lines 0-15 keep their numbers and
  vectors; the nucleus routes each through the I/O APIC by the MADT's
  overrides (`irqroute.c`, pure: the override, else the ELCR, a level line with
  no stated polarity active low) and masks only level lines until the ack,
  because an I/O APIC drops an edge on a masked pin (PIT-102). MSI lines 16-47
  are new numbers on vectors 0x50-0x6F (`nu_irq_alloc_msi`); a Go driver waits
  on any line the same way.
- **A PCI driver asks once.** `pci.Setup` gives a function MSI-X, else MSI, else
  its INTx line, and claims it for the driver; `pci.Serve` runs the handler
  through `driver.Lines` either way. INTx behind bridges is the firmware's line
  register only: no `_PRT`.
- **The USB stack is two layers.** `drivers/xhci` owns the rings, contexts,
  ports and the event ring, and hands out `usb.Device` (control transfers) and
  `usb.Pipe` (interrupt and bulk). `kernel/usb` owns descriptors, `Bind` and
  the class drivers: HID boot keyboards become scancode set 1 for the PS/2
  path (so `/dev/input/event0`, the VT keys and the hang dump are unchanged),
  boot mice go to `Env.Mouse`, mass storage (bulk-only, SCSI) becomes `ub*`
  block devices. Buffers above the controller are plain Go memory, copied
  into DMA pages by the controller driver.
- **Disks are found by what is on them.** AHCI and NVMe are `block.Writer`s
  like IDE and virtio; every disk's partitions are read (GPT, a whole-disk file
  system, MBR) and mounted by label, and the boot disk is found on any
  interface.
- **The machine reports itself.** A hardware report (every PCI function with
  its driver or none, USB devices, disks, routes) goes to the screen, a file
  and the console's `hw`, because a real PC's tester has only the screen.

## Consequences
- `pc` boots as before; `q35` with OVMF and no legacy devices boots from a USB
  stick and runs the console, Doom and the desktop with a USB keyboard and
  mouse, volumes on USB, SATA and NVMe (`make usb-desktop-smoke`, `usb-smoke`,
  `storage-smoke`, `q35-smoke`).
- The PS/2 path is the keyboard's one path: a USB keyboard does not repeat keys
  by itself, and neither does Burrow (nor did it with QEMU's PS/2 keyboard).
- No external USB hubs, no USB 2 controllers (EHCI), no isochronous transfers,
  no `_PRT`, no Intel HD Audio or real network cards: each waits for a real
  machine's report that needs it (W28).
- A pulled stick's next read fails after the bulk transfer's 10 s timeout:
  QEMU sends no event for it; acting on the port's status change would fail it
  at once.
- Tests: `test_irqroute.c`, `test_acpi.c`'s overrides, `kt_irq_ioapic_routes`,
  `kt_irq_msi_self`, `TestMSIXVectors`, the AHCI and NVMe fakes with
  `blocktest`, `block/part`, `kernel/usb`'s descriptor, HID and storage tests
  (a fake stick over a RAM disk), `drivers/xhci`'s ring and context tests,
  `TestSetupClaims`, and the smokes above.
