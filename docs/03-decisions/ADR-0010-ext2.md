# ADR-0010: ext2 as Burrow's Unix file system

- **Status:** Accepted
- **Date:** 2026-11-04
- **Wave:** W19

## Context
W19 makes Burrow multi-user: files need owners, modes (with set-uid, set-gid and sticky), symbolic and hard links, and a place to keep them across reboots. Nothing Burrow mounts can hold them: the initrd is a tar (read-only, and packed `--owner=0`), FAT32 has no owners, modes or links, memfs forgets at power-off. The persistent `/home` of W13 and the root disk W19-S06 boots from need a disk format with Unix attributes, written by the kernel in Go, and checkable on the host the way FAT is checked with `fsck.fat` (W13-S04).

## Options
1. **ext2 (revision 1, no journal)**: Linux's file system of the 1990s, still mounted by every Linux, with a small, fully documented format (superblock, groups, bitmaps, inodes with indirect blocks, linear directories); e2fsprogs creates (`mke2fs -d`), inspects (`debugfs`) and repairs (`e2fsck`) it on any host. No journal: a crash can leave work for `e2fsck`, as FAT does now.
2. **ext4**: what Linux uses today; extents, a journal, checksums, flexible groups, 64-bit sizes. Several times the format to read and much more to write correctly; a journal needs ordering guarantees the block layer does not offer yet.
3. **Minix v3**: simpler than ext2 and Unix-shaped, but host tools are scarce, and it caps names and sizes in ways ports notice.
4. **An own format**: easiest to write, and no tool on the host could check it; every bug would be checked only against itself (PIT-55).

## Decision
We choose ext2 because it has every attribute W19 needs, its format is small enough to read and write in a few files of Go, and e2fsprogs gives an independent checker: the reader is tested only on volumes mke2fs made, and everything the writer makes must pass `e2fsck -fn`. `kernel/fs/ext2` refuses a volume with an incompatible feature it does not know (ext3's journal recovery, ext4's extents, 64-bit) and keeps one with an unknown read-only-compatible feature read-only. ext3/ext4 can follow: a journal and extents are additions to this format, not a different one.

## Consequences
- The kernel tells FAT from ext2 by the superblock's magic and mounts both by label (`BURROWHOME`, `QTDATA`, `X11DATA`, `BURROWROOT` from W19-S06).
- Host tests need e2fsprogs; they skip without it, and `make ext2-interop` (part of `make test`, CI installs e2fsprogs) makes them required.
- The kernel's images can be made without root on the build machine only by a tool of our own that sets owners (`kernel/cmd/mkext2`, W19-S06); `mke2fs -d` copies the builder's own uid.
- No journal: after a crash, the volume may need `e2fsck` before it is mounted read-write. Burrow does not check it at mount time.
