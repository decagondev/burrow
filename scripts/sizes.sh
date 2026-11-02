#!/usr/bin/env bash
# sizes.sh - how much room is left in each fixed-size boot component.
set -euo pipefail
B="${1:-build}"
used() { python3 -c "import sys;b=open(sys.argv[1],'rb').read()[:int(sys.argv[2])];print(len(b.rstrip(b'\0')))" "$1" "$2"; }
printf "stage1 : %5d / 446 bytes of code (partition table starts at 446)\n" "$(used "$B/stage1.bin" 510)"
printf "stage2 : %5d / 16384 bytes\n" "$(used "$B/stage2.bin" 16384)"
printf "loader : %5d / 262144 bytes\n" "$(stat -c %s "$B/loader.bin")"
[ -f "$B/nucleus.disk.elf" ] && printf "nucleus: %5d bytes on disk (ELF, no debug info)\n" "$(stat -c %s "$B/nucleus.disk.elf")"
true
