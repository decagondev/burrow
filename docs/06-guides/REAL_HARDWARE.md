# Burrow on a real PC

Burrow boots from UEFI firmware as well as from a BIOS (W26), and since W27 it drives the devices a PC of the last decade has: interrupts through the I/O APIC and MSI, SATA disks behind AHCI, NVMe disks, and USB keyboards, mice and sticks behind an xHCI controller. Two images can go on a USB stick. This guide explains how to make them, how to boot one, and what to send back. Everything here is tested on QEMU's `q35` machine with OVMF and no legacy devices (`make usb-desktop-smoke`); a real PC is the next test, and your report is how it happens. [WAVE-27-real-pc.md](../04-plan/waves/WAVE-27-real-pc.md) has the design, [WAVE-26-uefi.md](../04-plan/waves/WAVE-26-uefi.md) and [BOOT_PROTOCOL.md](../02-architecture/BOOT_PROTOCOL.md) the UEFI loader.

## Which image

| Image | Make it with | What it needs from the PC | What you get |
|---|---|---|---|
| `build/burrow-initrd-usb.img` (about 100 MiB) | `make busybox doom usb-initrd` | the firmware, the screen and a USB keyboard | the Go console, busybox and Doom, all in the initrd: no disk driver at all. The hardware report is printed on the screen before the prompt. **Try this one first.** |
| `build/burrow-usb.img` (about 260 MiB) | `make busybox x11 net-ports browser uefi-usb` | the above, and the stick read as a disk (USB storage) | a login on an ext2 root from the stick, the X desktop, the network ports. The hardware report is saved in `/var/log/hw.txt`. |

`build/burrow-usb.img` is a GPT disk with these partitions:

| Partition | Label | What it holds |
|---|---|---|
| 1, EFI system partition | `BURROWBOOT` (FAT32) | `\EFI\BOOT\BOOTX64.EFI`, the UEFI loader, plus `\burrow\`: the manifest, the nucleus, the Go kernel, the initrd, the kargs (`login`) and the video request |
| 2 | `BURROWROOT` (ext2) | a fresh root, made by `scripts/mkroot.py`: busybox, `/etc` with the `root` and `burrow` accounts, the set-user-id X server |
| 3 | `NETDATA` (FAT32) | `/opt/net`: curl, dropbear, NetSurf (when `make net-ports browser` ran) |
| 4 | `X11DATA` (FAT32) | `/opt/x11`: the X server, dwm, st, dmenu (when `make x11` ran) |

`build/burrow-initrd-usb.img` has only the first partition; its initrd also holds `/bin/busybox`, `/bin/doom` and `/opt/doom/freedoom1.wad`, and its kargs say `hwreport`.

The kernel reads the partitions of every disk it finds, whatever its interface (IDE, SATA, virtio, NVMe or USB), and mounts each volume by its label (`kernel/block/part`, `kernel/cmd/kernel/bootparts.go`). The boot partition, and any disk's EFI system partition, is named in the log and never mounted. **A FAT32 or ext2 volume with any other label is mounted at `/mnt`, read-write**, wherever it is: if the PC has an internal disk with such a volume whose data matters, don't run programs that write to `/mnt` (NTFS and other file systems are not mounted at all).

Try them in QEMU first. `make run-usb` boots `burrow-usb.img` with OVMF in a window: log in as `burrow` (password `burrow`) and run `startx`. `make usb-desktop-smoke` boots both images the way a modern PC would see them: `q35`, the stick on USB storage, a USB keyboard and mouse, no PS/2. They need the `ovmf` package.

## Write it to a stick

Use a stick of 512 MB or more. **Everything on the stick is erased.**

- **Linux or WSL with the stick passed through:** find the stick's whole-disk device with `lsblk` (for example `/dev/sdb`, not `/dev/sdb1`). Make sure it is the stick, then run:

  ```sh
  sudo dd if=build/burrow-initrd-usb.img of=/dev/sdX bs=4M conv=fsync status=progress
  ```

- **Windows:** use [Rufus](https://rufus.ie) and pick **DD Image mode** when it asks. balenaEtcher also writes the image as it is.
- **Don't use Ventoy.** Ventoy boots an image as a file inside its own partition, so the firmware does start Burrow's loader. But the kernel sees Ventoy's disk, not Burrow's partitions, and finds no root and no data volumes.

The images have no BIOS boot code: their MBR is only GPT's protective one. To boot Burrow on a BIOS-only PC, write `build/go.img` or another BIOS image instead.

## Boot it

1. **Turn Secure Boot off** in the firmware's setup. Burrow's loader is not signed, so firmware with Secure Boot on refuses to run it. Leave the setup's other options as they are.
2. Open the firmware's one-time boot menu (often F12, F11, F8 or Esc while the maker's logo shows) and choose the stick. It may be listed as "UEFI: <stick name>". The loader is `\EFI\BOOT\BOOTX64.EFI`, the path firmware tries for removable disks, so no boot entry is needed.

## What to expect

Everything below appears on the screen as well as on COM1. Most PCs have no serial port, so the screen is the report.

1. **The loader's lines:** `[efi] ...`, then the firmware's memory map, the ACPI tables, each payload's CRC and the screen's mode through GOP. The loader picks 1024x768 if the screen offers it, and otherwise keeps the firmware's mode. If it stops here with `[efi] PANIC: the firmware owns ...`, that firmware keeps a page Burrow needs at a fixed address (PIT-94).
2. **The nucleus's lines:** `[nucleus] ... ok` for each part, among them `[nucleus] irq: i/o apic ...` with the interrupt routes the ACPI tables gave, the CPUs coming online, and `BURROW:NUCLEUS:OK`. A crash here leaves `[trap] ...` or `PANIC` and the registers on the screen.
3. **The Go kernel:** the drivers' lines (`[xhci] INFO: port N: ...` for each USB device, `[ahci]`, `[nvme]`), the volumes it mounted, then the hardware report (the initrd stick) and the prompt: `burrow>` on the initrd stick, `burrow login:` on the full one.

The console's `hw` command prints the hardware report again at any time; `irqs` lists every interrupt line and how many interrupts each driver took.

What Burrow drives, and what it does not yet:

| A real PC has | Burrow since W27 |
|---|---|
| The I/O APIC, MSI and MSI-X | yes: the ISA lines through the I/O APIC (the MADT's overrides), PCI devices by MSI-X or MSI where they have it |
| A USB keyboard and mouse (xHCI) | yes: HID boot protocol on the xHCI's root ports; no USB hubs yet, so plug them into the PC itself, not a hub or a keyboard's own ports |
| The stick, a USB disk | yes: USB mass storage (bulk-only, SCSI), `uba`, `ubb`, ... |
| SATA through AHCI, NVMe | yes: `sda`.., `nvme0n1`.., their FAT32 and ext2 volumes mounted by label (see above) |
| PS/2, IDE | yes, as before |
| An Intel 82540EM or 82574L network card | yes (W28-S01): `eth0`; the 82574L is the e1000e |
| An Intel I210/I219, Realtek or Wi-Fi network card | no: the report lists it as "no driver" |
| HD Audio | no (AC'97 only) |
| A GPU | the firmware's framebuffer (GOP) only |

## What to send back

1. **A photo of the screen** where it stopped, or of the hardware report (the initrd stick prints it before `burrow>`; type `hw` to see it again, and scroll with the photo in parts if it is long).
2. From the full stick, if it booted: **the file `/var/log/hw.txt`** on its `BURROWROOT` partition (any Linux can read the ext2 volume), or a photo of `hw`'s output.
3. The PC's make and model, and the firmware's version if the setup shows it.

The report names the firmware, the CPUs, the interrupt controller, every PCI function with its driver or "no driver", every USB device and controller, the disks, the mounts and each interrupt line's count. Lines that matter most when something failed are `[efi] PANIC`, `[trap]` or `PANIC`, the last `[nucleus]` line, `[xhci] WARN` lines (a port that did not enumerate), and the report's "no driver" lines.
