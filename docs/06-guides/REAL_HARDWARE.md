# Burrow on a real PC

Since W26 Burrow boots from UEFI firmware as well as from a BIOS, and `make uefi-usb` writes one disk image that a USB stick can carry. This guide explains how to put that image on a stick and what to expect from a real PC today. The short version: the firmware path works, and you should see Burrow's own lines on the screen. But Burrow's drivers are still the ones for QEMU's `pc` machine, so on most PCs built in the last decade the kernel will start without finding its keyboard or its disks. Those drivers are [W27](../04-plan/waves/WAVE-27-real-pc.md)'s job. [WAVE-26-uefi.md](../04-plan/waves/WAVE-26-uefi.md) has the design, and [BOOT_PROTOCOL.md](../02-architecture/BOOT_PROTOCOL.md) has what the UEFI loader does.

## Make the image

From the repository, in WSL or Linux:

```sh
make busybox x11 net-ports browser   # once: the root's busybox, the X disk, the network ports, NetSurf
make uefi-usb                        # build/burrow-usb.img
```

`build/burrow-usb.img` (about 260 MiB) is a GPT disk with these partitions:

| Partition | Label | What it holds |
|---|---|---|
| 1, EFI system partition | `BURROWBOOT` (FAT32) | `\EFI\BOOT\BOOTX64.EFI`, the UEFI loader, plus `\burrow\`: the manifest, the nucleus, the Go kernel, the initrd, the kargs (`login`) and the video request |
| 2 | `BURROWROOT` (ext2) | a fresh root, made by `scripts/mkroot.py`: busybox, `/etc` with the `root` and `burrow` accounts, the set-user-id X server |
| 3 | `NETDATA` (FAT32) | `/opt/net`: curl, dropbear, NetSurf (when `make net-ports browser` ran) |
| 4 | `X11DATA` (FAT32) | `/opt/x11`: the X server, dwm, st, dmenu (when `make x11` ran) |

The kernel mounts the boot disk's partitions by label, just as it mounts separate disks (`kernel/cmd/kernel/bootparts.go`). It names the boot partition in the log and never mounts it.

Try it in QEMU first. `make run-usb` boots the image with OVMF and no other disk, in a window. Log in as `burrow` (password `burrow`) and run `startx`. The UEFI twins of the other run targets work the same way: `make run-unix UEFI=1`, `make run-dev UEFI=1`, `make run-doom UEFI=1`. They need the `ovmf` package.

## Write it to a stick

Use a stick of 512 MB or more. **Everything on the stick is erased.**

- **Linux or WSL with the stick passed through:** find the stick's whole-disk device with `lsblk` (for example `/dev/sdb`, not `/dev/sdb1`). Make sure it is the stick, then run:

  ```sh
  sudo dd if=build/burrow-usb.img of=/dev/sdX bs=4M conv=fsync status=progress
  ```

- **Windows:** use [Rufus](https://rufus.ie) and pick **DD Image mode** when it asks. balenaEtcher also writes the image as it is.
- **Don't use Ventoy.** Ventoy boots an image as a file inside its own partition, so the firmware does start Burrow's loader. But the kernel sees Ventoy's disk, not Burrow's partitions, and finds no root and no data volumes.

The image has no BIOS boot code: its MBR is only GPT's protective one. To boot Burrow on a BIOS-only PC, write `build/go.img` or another BIOS image instead.

## Boot it

1. **Turn Secure Boot off** in the firmware's setup. Burrow's loader is not signed, so firmware with Secure Boot on refuses to run it. Leave the setup's other options as they are.
2. Open the firmware's one-time boot menu (often F12, F11, F8 or Esc while the maker's logo shows) and choose the stick. It may be listed as "UEFI: <stick name>". The loader is `\EFI\BOOT\BOOTX64.EFI`, the path firmware tries for removable disks, so no boot entry is needed.

## What to expect

These lines appear on the screen as well as on COM1 (W26-S04). Most PCs have no serial port, so a photo of the screen is the report.

1. **The loader's lines:** `[efi] ...`, then the firmware's memory map, the ACPI tables, each payload's CRC and the screen's mode through GOP. The loader picks 1024x768 if the screen offers it, and otherwise keeps the firmware's mode. If it stops here with `[efi] PANIC: the firmware owns ...`, that firmware keeps a page Burrow needs at a fixed address (PIT-94); please report it.
2. **The nucleus's lines:** `[nucleus] ... ok` for each part, the CPUs coming online and `BURROW:NUCLEUS:OK`. A crash here leaves `[trap] ...` or `PANIC` and the registers on the screen.
3. **The Go kernel:** the console's banner on the framebuffer. From here on, what works depends on the hardware:

| A real PC usually has | Burrow today | So |
|---|---|---|
| A USB keyboard and mouse (xHCI) | PS/2 only (`drivers/ps2`) | You can't type, unless the firmware still emulates a PS/2 keyboard after the boot services end. Some do, most don't. |
| The stick itself, a USB disk | IDE (`drivers/ata`) and virtio only | No root and no data volumes. The kernel keeps the initrd it already has in memory as `/`. That initrd has no `/bin/login`, so the Go console's `burrow>` prompt comes up instead of a login. |
| SATA through AHCI, or NVMe | IDE only, or a SATA controller in "IDE"/"legacy" mode | Internal disks aren't seen, and Burrow never writes to them. |
| Interrupts through the I/O APIC and MSI | the 8259 in virtual-wire mode | Devices that interrupt only by message are silent. Most chipsets still have a working 8259 for the legacy lines. |
| An Intel e1000e, Realtek or Wi-Fi network card | e1000 (8254x) and virtio-net | No network. |
| HD Audio | AC'97 | No sound. |
| No serial port | COM1 for the log | The log is only on the screen. |

The screen (GOP's framebuffer), the CPUs (ACPI's MADT), the timers, the real-time clock, and powering off through ACPI need nothing a modern PC lacks. Expect the boot to reach the Go console's banner and stop there, waiting for a keyboard it can't hear.

## What to send back

Please report:

- a photo of the screen where it stopped;
- the PC's make and model;
- the firmware's version, if the setup shows it.

Lines that matter most are:

- `[efi] PANIC`, `[trap]` or `PANIC`;
- the last `[nucleus]` line;
- the `[efi] screen:` line.

W27 adds the I/O APIC and MSI, AHCI, NVMe, and USB keyboards, mice and storage. It also adds a first-boot report that lists what the kernel found.
