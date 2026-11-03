# W13 A Workbench  (M13)  — status: in progress

Goal: write, save and build code *inside* Burrow. Demo: in `make run-dev`, open `/home/fib.c` in `vi` on the screen console, edit it, save, compile it with `tcc`, run it, power off, boot again, and the file is still there. The same for a Lua script. Stories: E13 ([BACKLOG.md](../BACKLOG.md)).

Where we start (after W12): busybox (`ash`) and the ports run; raw-mode terminals work (`TCGETS`/`TCSETS`, `TIOCGWINSZ`); but the screen console prints escape codes as text, the only writable place is `/tmp` (RAM, gone at power-off), the FAT32 disk is read-only, and the ATA driver cannot write a sector.

Design (continues W8-W12: drivers and file systems are Go policy; the nucleus does not change):
- **The editor is busybox `vi`,** switched on in `user/busybox.config`: no new download, and it needs only the terminal calls Burrow has. A second editor with syntax highlighting (kilo) is optional and needs a go-ahead to download.
- **The screen console becomes a terminal** for the subset editors and shells use: cursor positioning and movement, erase in line and screen, reverse video and the 8 ANSI colours, cursor show/hide, a visible cursor, and a scrolling region. The serial line already is one, so `vi` works there from S01.
- **Writing goes all the way down.** `block.Device` gains a write side (ATA PIO `WRITE SECTORS` plus `FLUSH CACHE`); `kernel/fs/fat` learns to create, grow, shrink, rename and remove files and directories, and a FAT volume mounts read-write as a `vfs.Writable`. Every change is checked against `fsck.fat` and mtools, as reading was in W8.
- **A home that persists.** `build/home.img` is created once and never rebuilt by `make`; `make run-dev` boots an image with busybox, `vi`, Lua, TinyCC and that disk at `/home`.

### [x] W13-S01 busybox vi (E13)
Enable `VI` and its features in `user/busybox.config`; `vi` edits a file in `/tmp` over the serial terminal. busybox-smoke drives it: open, insert, `:wq`, `cat` shows the text.
As built: `vi` needed one call Burrow lacked, `ftruncate` (and `truncate` with it), to save. `FEATURE_VI_ASK_TERMINAL` stays off until the screen console can answer `ESC[6n`. `make run-busybox` boots the busybox image interactively; until S02, use `vi` in the terminal that shows the serial line.

### [x] W13-S02 The screen console speaks VT100 (E13)
`kernel/gfx`: a terminal state machine for `ESC [` sequences (CUP, CUU/CUD/CUF/CUB, ED, EL, SGR 0/1/7/30-37/40-47, DECTCEM, DECSTBM) and a drawn cursor; host tests feed byte streams and check the cell grid. A screen check of `vi` on the console.
As built: the console keeps a cell grid (`gfx/console.go`) and parses in `gfx/vt.go`; also CHA/VPA, insert/delete line, reverse index, ESC 7/8, the alternate screen (`?1049`, so the shell's lines come back after `vi`), bright colours; charset selection, OSC and unknown sequences are swallowed. The cursor is a line in the two pixel rows under each glyph, so it never hides a character. Queries (`ESC[6n`) are not answered, so `FEATURE_VI_ASK_TERMINAL` stays off. `tests/smoke/expect/busybox.screen` checks `vi`'s screen.

### [x] W13-S03 Disk writes (E13)
`block.Writer` (`WriteSectors`, `Flush`), ATA PIO `WRITE SECTORS` (0x30) and `FLUSH CACHE` (0xE7) in `drivers/ata`, tested against `haltest` fakes and on QEMU by writing a scratch disk and reading it back.
As built: `block.Writer` and `block.WriterAt` (read-modify-write for partial sectors), a write half of the conformance test (`blocktest.RunWriter`) that the RAM disk and the ATA driver pass (the driver's simulated drive in `ata_test.go` takes data while DRQ is set). On QEMU, `make disk-smoke` boots with the kernel argument `disktest=hdb` (`kernel/cmd/kernel/disktest.go`): the kernel overwrites the last 64 sectors of a fresh blank scratch disk, flushes and reads them back, and `tests/smoke/checkdisk.py` finds the pattern in the image file on the host.

### [x] W13-S04 Writable FAT32 (E13)
`kernel/fs/fat`: allocate and free clusters, grow and truncate files, create and remove files and directories with long names, rename, update sizes and the FSInfo hints; mount read-write behind `vfs.Writable`. `make fat-interop` gains the other direction: images changed by the kernel's code pass `fsck.fat`, and mtools reads what it wrote.
As built: `fs/fat/write.go` (clusters: allocate from a rotating hint, grow and shrink chains with undo on `ENOSPC`, every FAT copy written per changed sector, FSInfo kept honest, a flush per operation) and `fs/fat/dirwrite.go` (`WriteFile`, `Mkdir`, `Remove`; long names with unique `NAME~N` aliases; a full directory grows by a zeroed cluster). `readDir` and the writer share one directory scanner. Order: data, then the entry, then the FAT, then the flush, so a crash can only leak clusters. `mkfat -put/-mkdir/-rm` run the same code on image files, and `make fat-interop` applies a series of edits to a `mkfs.fat` image (4 KiB clusters) and to ours, then `fsck.fat` and mtools check them. The kernel mounts `hdb` at `/mnt` read-write when it can be written. Rename is S05.

### [ ] W13-S05 Saving through the system calls; make run-dev (E13)
Writes on a FAT file reach the disk (`fsync`, `fdatasync`, and on close), `rename`/`renameat`, `ftruncate`, `chmod` as accepted no-ops where FAT has no field; `build/home.img` mounted at `/home`; `make run-dev` (busybox, `vi`, Lua, TinyCC, `/home`). A smoke test edits, saves, reboots the same disk and reads the file back.

### [ ] W13-S06 kilo, a small editor with syntax highlighting (E13) — optional, needs a go-ahead to download it

Exit: **M13 demo — a workbench.** `make clean test` green; DEVLOG wave entry; PORTING.md and a short "working on Burrow" guide.
