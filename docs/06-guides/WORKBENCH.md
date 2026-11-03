# Working on Burrow

Burrow can be used as a small development machine (W13): edit files with `vi`, compile C with TinyCC, run Lua, and keep your work on a disk that survives a power-off. This guide is the short version; [PORTING.md](PORTING.md) is about bringing more software over.

## Start it

From the repository, in WSL or Linux:

```sh
make busybox ports      # once: downloads (pinned) and builds busybox, Lua, TinyCC
make run-dev            # boots in a QEMU window, with your home disk attached
```

`make run-dev` opens a window (`DEV_DISPLAY=sdl` picks another QEMU display, `DEV_DISPLAY=none` keeps only the serial terminal). The first run creates `burrow-home.img` in the repository root from the starter files in `workbench/`. `make` never overwrites it; delete it to start again. It is git-ignored: your files stay yours.

## A session

At the `burrow>` console:

```text
run /bin/busybox sh      a shell (busybox ash: pipes, redirects, history, tab completion)
cd /home                 your files
vi hello.c               edit: i inserts, Esc stops, :wq saves and quits, :q! quits without saving
tcc -static -o hello hello.c
./hello 30               run what you compiled
lua hello.lua            run a Lua script (lua alone starts the REPL; Ctrl-D leaves it)
mv, cp, rm, mkdir, ls    the usual tools (busybox)
exit                     back to burrow>
poweroff                 end the session; /home is already on the disk
```

## What to know

- **Where files live.** `/home` is the FAT32 disk `burrow-home.img` (it mounts there because its label is `BURROWHOME`). `/tmp` is in memory and is gone at power-off. `/` (the programs, `/etc`) is read-only.
- **Every save is on the disk** when the editor says so: each write goes through to the disk image and is flushed. There is no `sync` to remember.
- **FAT32 rules apply in `/home`:** names are case-insensitive (`Hello.c` and `hello.c` are one file), there are no permissions or symbolic links (`chmod` succeeds and changes nothing), and a file holds at most 4 GiB.
- **Compile with `tcc -static`:** Burrow has no dynamic loader. `tcc -run` is not available (it needs a dynamic `tcc`).
- **Typing.** The screen console is a VT100 subset (enough for `vi` and the shell); the terminal you started QEMU from shows the same session on the serial line, and you can type in either.
- **Outside Burrow,** the home disk is an ordinary FAT32 image: `mtype -i burrow-home.img ::/hello.c` reads a file, `mcopy -i burrow-home.img file.c ::/` puts one in (with QEMU stopped), and `fsck.fat -n burrow-home.img` checks it.
