#!/usr/bin/env python3
"""mkroot.py - Burrow's ext2 root disk (W19-S06): busybox with a link per
applet in /bin, /etc from the initrd plus passwd, group and shadow, /root,
/home/burrow, and the mount points /tmp, /dev, /mnt. The volume is made by
mke2fs -d (label BURROWROOT, which the kernel mounts at /), then every
inode gets its owner and mode from here with debugfs, so the image is the
same whoever builds it and wherever the tree lived (WSL's /mnt/c keeps no
modes). Accounts: root (password "root") and burrow (uid 1000, password
"burrow"): a test machine's. Never writes outside a temporary directory
and the output image.

Usage: scripts/mkroot.py <busybox> <etc-dir> <out.img> [size]
Docs: docs/06-guides/UNIX.md, docs/04-plan/waves/WAVE-19-unix.md (S06).
"""
import os
import shutil
import subprocess
import sys
import tempfile

SBIN = ['/usr/sbin', '/sbin']
SALT = 'burrowsalt'  # fixed: the same image every build
USERS = [  # name, uid, gid, password, home
    ('root', 0, 0, 'root', '/root'),
    ('burrow', 1000, 1000, 'burrow', '/home/burrow'),
]
SPECIAL = {  # path: (mode, uid, gid)
    'root': (0o040700, 0, 0),
    'home/burrow': (0o040700, 1000, 1000),
    'etc/shadow': (0o100600, 0, 0),
    'bin/busybox': (0o104755, 0, 0),  # set-uid root: su and passwd need it
    'tmp': (0o041777, 0, 0),
    'etc/dropbear': (0o040700, 0, 0),  # W24-S02: dropbear -R makes its host keys here
}
PROFILE = 'export PATH=/bin:/opt/net/bin\n'  # /opt/net: the NETDATA disk's programs (W24)


def tool(name):
    for d in os.environ.get('PATH', '').split(':') + SBIN:
        p = os.path.join(d, name)
        if os.access(p, os.X_OK):
            return p
    sys.exit('mkroot: %s not found (install e2fsprogs and openssl)' % name)


def crypt(password):
    out = subprocess.run([tool('openssl'), 'passwd', '-6', '-salt', SALT, password],
                         check=True, capture_output=True, text=True)
    return out.stdout.strip()


def etc_files(etc_src, etc):
    shutil.copytree(etc_src, etc, dirs_exist_ok=True)
    with open(os.path.join(etc, 'passwd'), 'w') as f:
        for name, uid, gid, _, home in USERS:
            f.write('%s:x:%d:%d:%s:%s:/bin/sh\n' % (name, uid, gid, name, home))
    with open(os.path.join(etc, 'group'), 'w') as f:
        for name, _, gid, _, _ in USERS:
            f.write('%s:x:%d:\n' % (name, gid))
    with open(os.path.join(etc, 'shadow'), 'w') as f:
        for name, _, _, password, _ in USERS:
            f.write('%s:%s:19000:0:99999:7:::\n' % (name, crypt(password)))
    with open(os.path.join(etc, 'profile'), 'w') as f:
        f.write(PROFILE)


def tree(root, busybox, etc_src):
    for d in ['bin', 'etc', 'etc/dropbear', 'root', 'home/burrow', 'tmp', 'dev', 'mnt']:
        os.makedirs(os.path.join(root, d))
    shutil.copy(busybox, os.path.join(root, 'bin/busybox'))
    applets = subprocess.run([busybox, '--list'], check=True, capture_output=True, text=True).stdout.split()
    for a in applets:
        if a != 'busybox':
            os.symlink('busybox', os.path.join(root, 'bin', a))
    etc_files(etc_src, os.path.join(root, 'etc'))


def program(path):
    with open(path, 'rb') as f:
        head = f.read(4)
    return head == b'\x7fELF' or head[:2] == b'#!'


def debugfs_script(root):
    lines = []
    for dirpath, dirnames, filenames in os.walk(root):
        dirnames.sort()
        for name in sorted(dirnames) + sorted(filenames):
            full = os.path.join(dirpath, name)
            rel = os.path.relpath(full, root).replace(os.sep, '/')
            if os.path.islink(full):
                mode, uid, gid = None, 0, 0
            elif os.path.isdir(full):
                mode, uid, gid = 0o040755, 0, 0
            else:
                mode, uid, gid = (0o100755 if program(full) else 0o100644), 0, 0
            mode, uid, gid = SPECIAL.get(rel, (mode, uid, gid))
            lines += ['sif /%s uid %d' % (rel, uid), 'sif /%s gid %d' % (rel, gid)]
            if mode is not None:
                lines.append('sif /%s mode 0%o' % (rel, mode))
    return '\n'.join(lines) + '\n'


def main():
    busybox, etc_src, out = sys.argv[1], sys.argv[2], sys.argv[3]
    size = sys.argv[4] if len(sys.argv) > 4 else '64M'
    work = tempfile.mkdtemp(prefix='burrow-root-')
    try:
        root = os.path.join(work, 'root')
        tree(root, busybox, etc_src)
        if os.path.exists(out):
            os.remove(out)
        subprocess.run([tool('mke2fs'), '-q', '-F', '-t', 'ext2', '-L', 'BURROWROOT', '-E', 'root_owner=0:0',
                        '-d', root, out, size], check=True)
        script = os.path.join(work, 'owners.debugfs')
        with open(script, 'w') as f:
            f.write(debugfs_script(root))
        subprocess.run([tool('debugfs'), '-w', '-f', script, out], check=True, capture_output=True)
        subprocess.run([tool('e2fsck'), '-fn', out], check=True, capture_output=True)
    finally:
        shutil.rmtree(work)


if __name__ == '__main__':
    main()
