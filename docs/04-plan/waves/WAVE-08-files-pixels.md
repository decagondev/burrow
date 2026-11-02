# W8 Files and Pixels  (M8)  — status: in progress

Goal: the Go kernel has a file tree (initrd, a writable `/tmp`, device files, a FAT32 data disk) and draws on a linear framebuffer. Demo: `cat /etc/motd` at the console; a gradient and a text console on the screen (checked with QEMU's `screendump`). Stories: E07.F01-F06 ([BACKLOG.md](../BACKLOG.md)).

Design: the VFS is built on Go's `io/fs` (`fs.FS`, `fs.File`, `fs.ReadDirFile`), so every file system is checked by the standard `testing/fstest.TestFS` conformance suite (Liskov), plus a small write extension for ramfs and devfs. W9-W10 put file descriptors on top.

### [x] W8-S01 Nucleus calls for the Go kernel's boot data and MMIO (E07.F01, E07.F04)
`nu_bootinfo` (0x1002, already reserved) copies boot_info v2; `nu_mmio_map` (0x1005, new) maps device memory and returns its virtual address. Go: `arch/amd64` wrappers and `PhysBytes`; `kernel/bootinfo` parses boot_info and the module table (pure, host-tested against bytes laid out like the C structs). Console: `modules`.

### [x] W8-S02 initrd + tarfs (E07.F01)
`initrd/` in the repo is packed by the Makefile into `build/initrd.tar` (ustar) and shipped as the `initrd` payload on `go.img`. `kernel/fs/tarfs`: read-only `fs.FS` over the archive bytes (stdlib `archive/tar`), checked with `fstest.TestFS`.

### [x] W8-S03 VFS + ramfs (E07.F02)
`kernel/vfs`: a mount table that is itself an `fs.FS` (longest-prefix mounts, path cleaning, `ReadDir` showing mount points), and `vfs.WriteFile/Mkdir/Remove` for file systems that can write (`vfs.Writable`). ramfs for `/tmp` is a writable `fs/memfs` (no package of its own). Both pass `fstest.TestFS`. Console: `ls`, `cat`, `write <file> <text>`, `mkdir`, `rm`, `mounts`.

### [x] W8-S04 devfs (E07.F03)
`kernel/fs/devfs`: `/dev/null`, `/dev/zero`, `/dev/console` (writes reach the console), and later `/dev/fb0`, `/dev/input/event0`. Device files are registered by drivers.

### [x] W8-S05 Framebuffer: BGA + text console (E07.F04)
`drivers/bga`: finds PCI `1234:1111`, sets 1024x768x32 through the Bochs dispi ports (`0x1CE/0x1CF`), maps BAR0 with `nu_mmio_map`. `kernel/gfx`: a pure `Framebuffer` (pitch-aware, PIT-27) with fills, a gradient and an embedded bitmap font; a text console that mirrors the serial console. `make go-smoke` saves a `screendump` and checks pixels.

### [x] W8-S06 Block layer + ATA PIO in Go (E07.F05)
`kernel/block`: `Device` (sector size, count, `ReadAt`), a RAM disk, and one conformance test both must pass. `drivers/ata`: IDENTIFY and 28-bit LBA PIO reads on the primary channel (`hal.PortIO16`). On target: read the boot disk's manifest sector and check its magic.

### [ ] W8-S07 FAT32 read-only (E07.F06)
`tools/mkfat` builds a FAT32 image from a directory (also used by host tests); `make` puts `build/data.img` on the primary slave. `kernel/fs/fat`: `fs.FS` over a `block.Device`, `fstest.TestFS` on a generated image; mounted at `/mnt`.

Exit: **M8 demo — files and pixels.** `make clean test` green; DEVLOG wave entry.
