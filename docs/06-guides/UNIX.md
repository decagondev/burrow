# Users and permissions

Burrow is a multi-user system (W19): people log in, files have owners and permissions, links work, and an ext2 disk keeps it all. This guide is the short version; [WAVE-19-unix.md](../04-plan/waves/WAVE-19-unix.md) has the details and [POSIX_COVERAGE.md](../02-architecture/POSIX_COVERAGE.md) the rules each call follows.

## Start it

From the repository, in WSL or Linux (it needs e2fsprogs and openssl on the host):

```sh
make busybox           # once: downloads (pinned) and builds busybox, with the login tools
make run-unix          # boots in a QEMU window from an ext2 root disk, to a login: prompt
```

The first run creates `burrow-root.img` in the repository root (`scripts/mkroot.py`): busybox and a link per applet in `/bin`, `/etc` with two accounts, `/root`, `/home/burrow`. It is git-ignored and `make` never overwrites it; delete it to start again. The kernel mounts it at `/` in the initrd's place because its label is `BURROWROOT`; `/tmp` (mode 1777) and `/dev` stay mounted over it.

| Account | Password | uid | Home |
|---|---|---|---|
| `root` | `root` | 0 | `/root` (0700) |
| `burrow` | `burrow` | 1000 | `/home/burrow` (0700) |

These are a test machine's passwords. Change them with `passwd` once you are in.

## A session

```text
burrow login: burrow
Password:                    (nothing echoes)
$ id                         uid=1000(burrow) gid=1000(burrow) groups=1000(burrow)
$ echo x >> /etc/passwd      -sh: can't create /etc/passwd: Permission denied
$ ln -s /etc/hostname n      a symbolic link; readlink n, cat n
$ su                         root's password; # is root's prompt
# adduser -D alice           a new account (uid 1001, its own group, /home/alice)
# passwd alice               give it a password
# exit                       back to burrow
$ exit                       logged out: the burrow> console; poweroff ends the session
```

Everything is on the disk at once: after a reboot alice logs in, and burrow's home is closed to her (`ls: can't open '/home/burrow': Permission denied`). The tools are busybox's: `id`, `su`, `passwd`, `adduser`/`deluser`, `addgroup`/`delgroup`, `chmod`, `chown`, `chgrp`, `stat`, `ln`, `readlink`.

## How it works

- **Who you are** is a process's credentials (`kernel/proc/cred.go`): real, effective, saved and file-system user and group ids, and supplementary groups, changed with Linux's rules. busybox is set-user-id root on the root disk; it keeps root only for the applets that need it (`su`, `passwd`) and drops it for the rest.
- **Who may do what** is checked as Linux checks it (`kernel/proc/perm.go`): one class of a file's bits counts (owner, else group, else others); every directory on a path needs search permission; creating and removing need write on the directory, and the sticky bit (`/tmp`) keeps others' files from you. Root passes, except that it too needs an execute bit to run a program.
- **Paths** are walked a component at a time (`vfs.Resolve`): links are followed, `..` after a link goes back to where the link led, and permission is checked on every directory the walk passes, a link's included.
- **Files keep owners** on ext2 (`kernel/fs/ext2`), in `/tmp` (memfs) and on the initrd (tar headers). FAT keeps none: there root owns every file and every file is 0755, as Linux's vfat shows it.
- **Logging in**: go-unix.img's kernel arguments say `login`, so the kernel runs `/bin/login` (busybox) on the console before its own `burrow>` prompt; login checks `/etc/shadow` (SHA-512), sets the ids with `setgroups`, `setgid` and `setuid`, and starts `/bin/sh`.

## Outside Burrow

The root disk is an ordinary ext2 image (with QEMU stopped): `debugfs -R "cat /etc/passwd" burrow-root.img` reads a file, `debugfs -R "ls -l /home" burrow-root.img` lists with owners, and `e2fsck -fn burrow-root.img` checks it. `make unix-smoke` types the session above over two boots on a fresh copy and runs those checks.

## Limits

- No journal: after a crash, run `e2fsck -fy burrow-root.img` before the next boot.
- Record locks (`fcntl F_SETLK`) are granted and not kept; nothing on Burrow contends for one yet.
- A program running from the disk must not be rewritten or deleted while it runs (no `ETXTBSY` yet): it reads its pages from the disk as it goes.
- Terminals are not owned by whoever logs in, and `/dev/shm` is not checked; there is no `/proc`, so tools that look there (`ps`, `w`) are not built.

## Processes and logs (W23)

busybox's process tools read `/proc`: `ps`, `top` (CPU time is counted by the nucleus), `free`, `uptime`, `pidof`, `pgrep`, `pkill`, `killall`, `tty`, `df`, `mount` (it lists; mounting is not served), `fuser` and `lsof`. A process's `/proc/<pid>/fd`, `exe`, `cwd` and `maps` are readable by its own user and root only.

`syslogd` takes the messages programs send to `/dev/log` (`logger`, or `syslog(3)`): `syslogd -O /tmp/messages` writes them to a file (the initrd has no writable `/var`; an ext2 root does), and `syslogd -C16` keeps them in a 16 KiB circular buffer in shared memory, which `logread` prints (`logread -f` follows it).
