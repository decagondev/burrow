#!/usr/bin/env python3
"""mkinitrd.py - pack a directory into the initrd's ustar archive (W19-S04).

Reproducible: names sorted, owner root (0:0), every time 0. Modes are set
here, not taken from the build tree, which may live on a file system that
keeps no Unix modes (WSL's /mnt/c): directories 0755; files that are
programs (ELF, or a "#!" script) 0755; everything else 0644. Since W19-S04
the kernel checks execute permission, so a program packed 0644 cannot run.
Explicit PATH=MODE[:UID:GID] arguments override (octal mode, e.g.
etc/shadow=0600 or bin/su=4755). Never follows symbolic links out of the tree.

Usage: scripts/mkinitrd.py <root-dir> <out.tar> [PATH=MODE[:UID:GID]...]
Docs: docs/04-plan/waves/WAVE-19-unix.md (S04).
"""
import io
import os
import sys
import tarfile


def program(path):
    with open(path, 'rb') as f:
        head = f.read(4)
    return head == b'\x7fELF' or head[:2] == b'#!'


def overrides(args):
    out = {}
    for a in args:
        name, spec = a.split('=', 1)
        parts = spec.split(':')
        mode = int(parts[0], 8)
        uid, gid = (int(parts[1]), int(parts[2])) if len(parts) == 3 else (0, 0)
        out[name.strip('/')] = (mode, uid, gid)
    return out


def entries(root):
    out = []
    for dirpath, dirnames, filenames in os.walk(root):
        dirnames.sort()
        rel = os.path.relpath(dirpath, root).replace(os.sep, '/')
        if rel != '.':
            out.append((rel, True))
        for f in sorted(filenames):
            out.append((f if rel == '.' else rel + '/' + f, False))
    return sorted(out)


def main():
    root, dest, special = sys.argv[1], sys.argv[2], overrides(sys.argv[3:])
    with tarfile.open(dest, 'w', format=tarfile.USTAR_FORMAT) as tar:
        for name, is_dir in entries(root):
            path = os.path.join(root, name)
            info = tarfile.TarInfo('./' + name)
            info.mtime, info.uid, info.gid, info.uname, info.gname = 0, 0, 0, '', ''
            if is_dir:
                info.type, info.mode = tarfile.DIRTYPE, 0o755
            else:
                info.mode = 0o755 if program(path) else 0o644
                info.size = os.path.getsize(path)
            if name in special:
                info.mode, info.uid, info.gid = special[name]
            if is_dir:
                tar.addfile(info)
            else:
                with open(path, 'rb') as f:
                    tar.addfile(info, io.BytesIO(f.read()))


if __name__ == '__main__':
    main()
