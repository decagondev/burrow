# W11 DOOM  (M11)  — status: done

Goal: an unmodified static musl build of Doom (doomgeneric with `user/doom/doomgeneric_burrow.c`) runs on Burrow, draws through `/dev/fb0` and reads the keyboard through `/dev/input/event0`. Demo: `run /bin/doom` shows the title screen; Enter starts E1M1. Stories: E10.F01-F05 ([BACKLOG.md](../BACKLOG.md)), [ADR-0006](../../03-decisions/ADR-0006-linux-abi-userland.md).

Design (continues W9-W10: the nucleus stays mechanism; devices are Go policy):
- **Devices speak Linux.** `/dev/fb0` answers the fbdev subset Doom uses and `/dev/input/event0` delivers `struct input_event`, so the backend can be written and checked against Linux headers. A device file may offer more than read and write through small interfaces that `kernel/proc` looks for: `Poller` (readiness, which also gives every such file `O_NONBLOCK`), `Ioctler` (its own ioctls) and `Mappable` (physical memory for `mmap(MAP_SHARED)`).
- **Device memory is not process memory.** A mapped framebuffer page belongs to the device: unmapping or exiting never frees it, and fork shares it instead of copying.
- **One owner for the screen and the keyboard.** While a program holds `/dev/fb0` open, the text console stops drawing on the screen; it repaints when the last holder closes. While `/dev/input/event0` is open, keys go to it and not to the console (Linux's `EVIOCGRAB` without asking).
- **Downloads are explicit.** doomgeneric's source and a WAD are fetched only after a go-ahead, pinned by commit or sha256, cached outside the tree, and their images skip when absent (as busybox does).

### [x] W11-S01 /dev/fb0: fbdev ioctls and MAP_SHARED device memory (E10.F01)
`FBIOGET_VSCREENINFO`, `FBIOGET_FSCREENINFO` (`line_length` = the real pitch, PIT-27), `FBIOPUT_VSCREENINFO` for the current mode, `FBIOPAN_DISPLAY` at 0; `mmap` of the framebuffer; the console steps aside while it is open. `user/fbtest.c` draws colour bars; a screen check reads them back.
As built: `kernel/fbdev` (the device), `kernel/proc/devices.go` (`Ioctler`, `Mappable`, `O_NONBLOCK` through `Poller`), device pages in `proc.Mem` (`MapDevice`), `devfs.Opener` for per-open handles. `make devices-smoke` boots `go.img` and checks the bars and the console's repaint (`tests/smoke/expect/devices.screen`). User C programs see the Linux UAPI headers through `build/uapi` (links to the host's `linux-libc-dev`).

### [x] W11-S02 /dev/input/event0: evdev from PS/2 (E10.F02)
Scancode set 1 (with `E0` prefixes) to Linux key codes; press, release and autorepeat as `EV_KEY` values 1/0/2 followed by `EV_SYN`; `O_NONBLOCK` reads (`EAGAIN`), `poll`; the device grabs the keyboard while open. `user/evtest.c` prints events; go-smoke types into it. The evdev → Doom key map gets a unit test (BUG-0004).
As built: `kernel/evdev` (a queue per open file, `EVIOCGNAME EVIOCGVERSION EVIOCGID EVIOCGRAB`); the PS/2 driver hands every byte to `driver.Env.Scancodes` first and sends nothing to the console while it reports a grab. The typing check is in `make devices-smoke` (evtest), not go-smoke. The Doom key-map test moves to S03, where the backend is compiled.

### [x] W11-S03 doomgeneric with the Burrow backend (E10.F03)
`make doom` fetches doomgeneric at a pinned commit and builds it with musl and the snippet backend.
As built: `scripts/doom.sh` pins doomgeneric `e257a09` (tarball sha256 too), compiles the engine (its Makefile's list minus X11) with `-O2` and the backend, now `user/doom/doomgeneric_burrow.c`, and runs `tests/unit/doom/keymap_test.c` (BUG-0004's regression test) on the host. The Linux-VM rehearsal of the plan was skipped: the backend uses only the fbdev/evdev subset that S01-S02 test directly.

### [x] W11-S04 A WAD (E10.F04)
Freedoom phase 1 (freely redistributable), pinned by sha256, on the image Doom boots from.
As built: Freedoom 0.13.0, checked against the sha256 in its signed CHECKSUM file; `freedoom1.wad` (28.8 MB) goes on `build/doom-data.img`, a 48 MiB FAT32 disk mounted at `/mnt`, so the initrd stays small.

### [x] W11-S05 Doom runs (E10.F03)
`make doom-smoke`: the title screen appears (screen check), Enter and the menu keys start E1M1.
As built: `run /bin/doom -iwad /mnt/freedoom1.wad` on `build/go-doom.img`. The only missing kernel piece was `readv` (musl's stdio). `doom.keys` dumps the title, presses Enter four times into E1M1, dumps the status bar, walks, and quits through the menu back to the console prompt; `sendkeys.py` gained `sleep` for steps that only the screen shows. When a program takes `/dev/fb0` the console now clears the screen to black.

### [x] W11-S06 Frames per second (E10.F05)
An FPS counter; measure, then fix the hot spots (RISKS R-07).
As built: Doom's own `-timedemo demo1` is the counter. `make doom-bench` (about 70 s, not in `make test`) plays it and fails below 20 fps: 100-104 fps under QEMU TCG, three times Doom's 35 tics per second, with every frame copied by the backend and no hot path in the nucleus. No tuning was needed.

Exit: **M11 demo — DOOM.** `make clean test` green; DEVLOG wave entry.

As built: S01 and S02 are their own commits; S03-S06 landed together because the first boot of Doom answered all four at once. Downloads follow busybox's pattern: pinned, cached in `~/.cache/burrow-doom`, and `make doom-smoke` skips without them (CI skips it).
