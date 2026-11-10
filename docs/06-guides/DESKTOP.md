# The Burrow desktop

Burrow runs an X11 desktop (W16): the TinyX server `Xfbdev` on `/dev/fb0`, the dwm window manager, st terminals and the dmenu launcher, all static programs on a FAT disk mounted at `/opt/x11`. This guide is how to use it; [WORKBENCH.md](WORKBENCH.md) is the console workbench underneath, and [PORTING.md](PORTING.md#x11-w16) is how the X stack is built.

## Start it

From the repository, in WSL or Linux:

```sh
make busybox ports doom x11   # once: downloads (pinned) and builds what the desktop needs
make run-desktop              # boots in a QEMU window: your home disk and the X disk
```

At the `burrow>` console:

```text
run /opt/x11/bin/startx       or from busybox sh: startx
```

`startx` starts the server, runs your session script and, when it ends, stops the server; the console comes back as it was. `make run-desktop` uses the same `burrow-home.img` as `make run-dev`, so your files are the same on both.

## dwm's keys

dwm tiles windows: the newest is the master on the left, the others stack on the right. Alt is dwm's key (`Mod`).

| Keys | What they do |
|---|---|
| Alt+Shift+Enter | a terminal (st running busybox `sh`, in `/home`) |
| Alt+p | dmenu: type part of a program's name, Enter starts it (`doom`, `st`, `xhello`) |
| Alt+j, Alt+k | focus the next, the previous window |
| Alt+Enter | swap the focused window with the master |
| Alt+h, Alt+l | a narrower, a wider master |
| Alt+t, Alt+f, Alt+m | tiled, floating, one window at a time (monocle) |
| Alt+Shift+Space | float the focused window, or tile it again |
| Alt+1 ... Alt+9 | show tag 1-9; Alt+Shift+1-9 moves the focused window there |
| Alt+Shift+c | close the focused window |
| Alt+Shift+q | quit dwm: the session ends, the console comes back |

The screen is whatever mode the boot image asked the firmware for (W21: 1024x768 by default; `make fb-smoke` runs X at 1280x1024 with a padded scanline), and Xfbdev takes its size from `/dev/fb0`, so nothing in the session should assume 1024x768.

Doom asks for a fixed 640x400, so dwm floats it above the tiled windows instead of tiling it. Its keys are Doom's: arrows, Ctrl fires, Space opens doors, Escape is the menu (Quit Game ends it).

## Your session

`startx` runs the first of these it finds: `$HOME/.xinitrc`, `/home/.xinitrc`, `/opt/x11/etc/xinitrc` (which only starts dwm). The starter home disk has a `.xinitrc`:

```sh
cd /home
exec /opt/x11/bin/dwm
```

The session lasts as long as the script, so dwm is last, with `exec`. To start programs with the desktop, put them before it with a trailing `&`, for example `/opt/x11/bin/st -e /bin/busybox sh &`. A home disk made before W16 has no `.xinitrc`; `startx` then uses the default, and you can write one with `vi /home/.xinitrc`.

## Adding programs

dmenu lists every program in `$PATH` (`/opt/x11/bin:/bin`, set by `/opt/x11/x11.env`). A program is an X client if it is a static x86-64 Linux binary linked with Xlib: build it the way `scripts/x11-apps.sh` builds `user/x11/*.c` (the ports' musl cross compiler and the libraries in `~/.cache/burrow-x11/sysroot`), put it in `build/x11-root/bin`, and the next `make` puts it on the X disk. TinyCC on Burrow cannot build X programs yet: the X libraries are not in its sysroot.

## What TinyX leaves out

TinyX is a small X server from 2006, and Burrow's build switches more off:

- **No XKB.** Programs that require the keyboard extension fail or fall back; this is why Doom has its own X backend (`user/x11/doom/`).
- **No GLX or Xvideo.** MIT-SHM works since W18 (SysV shared memory): Doom draws through it at about three quarters of its fbdev frame rate; programs that do not use it send images through the socket, as before: Doom gets about a third of its fbdev frame rate in a window (`make x11-bench`), still above its 35 tics a second.
- **No XINERAMA.** dmenu says so on start and carries on; one screen is all there is.
- **One display, one screen** at 1024x768x24, no resizing (RandR) and no display manager; clients connect over the Unix socket `/tmp/.X11-unix/X0` (Burrow has no TCP until W17).
- **Fonts:** the core font `fixed` (and the cursor font), plus DejaVu through Xft and fontconfig for st, dmenu and dwm's bar.
- **Input:** the US keymap, read from the console with `KDGKBENT`, and the pointer through `/dev/input/mice` (the PS/2 mouse, and since W22 QEMU's tablet as moves; see below).

## Sound and the pointer (W22)

`make run-desktop` (and every run target) boots with an AC'97 card and QEMU's virtio tablet. The tablet is an absolute pointer: X's pointer sits where the host's is, with no click into the window to grab it and no Ctrl-Alt-G to let go (`startx` runs `xaccel` first, which turns X's acceleration off so the moves stay exact). `TABLET=0 make run-desktop` leaves only the PS/2 mouse, which needs the grab again.

Sound goes nowhere by default (`RUN_AUDIO ?= none`: the card is there and plays into nothing). To hear it, give QEMU a host backend:

```sh
make run-desktop RUN_AUDIO=pa     # PulseAudio; WSLg provides it on Windows
```

Doom from dmenu then plays its effects. Only one program plays at a time (`/dev/dsp` has one opener), and there is no music or recording.

## What to know

- **While X runs it owns the keyboard and the screen;** the serial terminal you started QEMU from still shows the console's output, so messages from programs started by the session appear there.
- **st sets its title** in dwm's bar from Xlib's locale data on the X disk (`XLOCALEDIR`).
- **If the server does not start,** `startx` prints the end of `/tmp/x.log`, the server's log.

## virtio by default (W23-S07)

The run targets attach their data disks as virtio-blk (`vda`, `vdb`, ... in the log, mounted by label as before) and their network card as virtio-net: both are much faster under QEMU (`make disk-bench`, `make net-bench`). `RUN_DISK_IF=ide` and `NIC=e1000` bring back the IDE disks and the e1000. The boot disk is IDE either way: the BIOS reads it.
