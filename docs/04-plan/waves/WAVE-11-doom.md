# W11 DOOM  (M11)  — status: in progress

Goal: an unmodified static musl build of Doom (doomgeneric with `snippets/userland/doomgeneric_burrow.c`) runs on Burrow, draws through `/dev/fb0` and reads the keyboard through `/dev/input/event0`. Demo: `run /bin/doom` shows the title screen; Enter starts E1M1. Stories: E10.F01-F05 ([BACKLOG.md](../BACKLOG.md)), [ADR-0006](../../03-decisions/ADR-0006-linux-abi-userland.md).

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

### [ ] W11-S03 doomgeneric with the Burrow backend (E10.F03) — needs a go-ahead to download its source
`make doom` fetches doomgeneric at a pinned commit and builds it with musl and the snippet backend.

### [ ] W11-S04 A WAD (E10.F04) — needs a go-ahead to download it
Freedoom phase 1 (freely redistributable), pinned by sha256, on the image Doom boots from.

### [ ] W11-S05 Doom runs (E10.F03)
`make doom-smoke`: the title screen appears (screen check), Enter and the menu keys start E1M1.

### [ ] W11-S06 Frames per second (E10.F05)
An FPS counter; measure, then fix the hot spots (RISKS R-07).

Exit: **M11 demo — DOOM.** `make clean test` green; DEVLOG wave entry.
