#!/usr/bin/env bash
# run_all.sh - regression tests: deliberately broken inputs MUST fail loudly.
#
# Each case = (name, how to break a copy of the image, expected serial text,
# expected QEMU exit status). Every bug in docs/living/BUGS_AND_MITIGATIONS.md
# that CAN be reproduced from an image gets a case here, named after its ID.
set -euo pipefail
IMG="${1:-build/disk.img}"
QEMU="${QEMU:-qemu-system-x86_64}"
WORK="$(dirname "$IMG")/regression"
mkdir -p "$WORK"
pass=0; failn=0

boot() { # boot <img> <log> -> sets STATUS
  set +e
  timeout 20 "$QEMU" -machine pc -cpu "${QEMU_CPU:-Broadwell-noTSX-IBRS}" -m 128M ${REG_SMP:+-smp $REG_SMP} \
    -drive "file=$1,format=raw,if=ide,index=0,media=disk" \
    -device isa-debug-exit,iobase=0xf4,iosize=0x04 \
    -serial "file:$2" -display none -no-reboot
  STATUS=$?
  set -e
}

flip_byte() { # flip_byte <file> <offset>
  python3 - "$1" "$2" <<'PY'
import sys
p, off = sys.argv[1], int(sys.argv[2], 0)
b = bytearray(open(p, 'rb').read()); b[off] ^= 0xFF; open(p, 'wb').write(b)
PY
}

case_run() { # case_run <name> <expected-text> <expected-status> <mutator...>
  local name="$1" want="$2" want_status="$3"; shift 3
  local img="$WORK/$name.img" log="$WORK/$name.log"
  cp "$IMG" "$img"
  if ! "$@" "$img"; then
    echo "  FAIL  $name (mutator $1 failed)"; failn=$((failn+1)); return
  fi
  boot "$img" "$log"
  if grep -qF -- "$want" "$log" && { [ "$want_status" = any ] || [ "$STATUS" -eq "$want_status" ]; }; then
    echo "  ok    $name"; pass=$((pass+1))
  else
    echo "  FAIL  $name (status=$STATUS, wanted '$want' / $want_status)"; tail -5 "$log"; failn=$((failn+1))
  fi
}

lba_of() { # lba_of <img> <manifest-name> -> first sector of that payload
  python3 - "$1" "$2" <<'PY'
import struct, sys
b = open(sys.argv[1], 'rb').read(1024)[512:]
count = struct.unpack_from('<H', b, 10)[0]
for i in range(count):
    off = 16 + 40 * i
    if b[off:off + 16].rstrip(bytes(1)).decode() == sys.argv[2]:
        print(struct.unpack_from('<I', b, off + 16)[0]); break
else:
    sys.exit('no manifest entry ' + sys.argv[2])
PY
}

# --- cases -------------------------------------------------------------------
# Loader bytes live at LBA 34. Flip one byte in the middle of its code.
corrupt_loader()   { flip_byte "$1" $((34*512 + 3000)); }
# Manifest magic at LBA 1 offset 0: stage2 must refuse with code F.
corrupt_manifest() { flip_byte "$1" $((1*512)); }
# stage2 magic 'S2' at LBA 2 offset 2: stage1 must refuse with code M.
corrupt_stage2()   { flip_byte "$1" $((2*512 + 2)); }
# One byte inside the nucleus text: the loader must refuse it before loading.
corrupt_nucleus()  { local lba; lba=$(lba_of "$1" nucleus) || return 1; flip_byte "$1" $((lba*512 + 0x1000 + 16)); }
# Rebuild the image with a "ktest" payload naming one test (nucleus/ktest.h).
with_ktest() { # with_ktest <test-name> <img>
  local b; b="$(dirname "$IMG")"
  echo "$1" > "$WORK/ktest-$1.txt"
  "$b/mkimage" -o "$2" -stage1 "$b/stage1.bin" -stage2 "$b/stage2.bin" -payload loader="$b/loader.bin" \
    -payload nucleus="$b/nucleus.disk.elf" -payload ktest="$WORK/ktest-$1.txt" >/dev/null
}
# A ud2 with no fixup must print the trap report and fail, not hang or reboot.
unhandled_ud()     { with_ktest trap_unhandled_ud "$1"; }
# W20-S06: a trampoline that halts (smp.c writes "jmp $" over it for this test)
# must not leave a machine quietly on one CPU: the AP is reported and the test fails.
broken_trampoline() { with_ktest smp_broken_trampoline "$1"; }
# Rebuild the image with the Go kernel and a "kargs" payload (kernel/cmd/kernel/kargs.go).
with_kargs() { # with_kargs <words> <img>
  local b; b="$(dirname "$IMG")"
  [ -f "$b/kernel.elf" ] || { echo "  (no $b/kernel.elf: make go-smoke first)"; return 1; }
  echo "$1" > "$WORK/kargs-${1%%=*}.txt"
  "$b/mkimage" -o "$2" -stage1 "$b/stage1.bin" -stage2 "$b/stage2.bin" -payload loader="$b/loader.bin" \
    -payload nucleus="$b/nucleus.disk.elf" -payload kernel="$b/kernel.elf" \
    -payload kargs="$WORK/kargs-${1%%=*}.txt" >/dev/null
}
# A Go panic prints itself and every goroutine, then exit_group fails the run.
go_panic()         { with_kargs panic=boot "$1"; }
# PIT-07: a nil write in Go is a #PF in Go text; the report must say so.
go_nil_fault()     { with_kargs fault=nil "$1"; }

echo "[regression]"
case_run REG-0001-loader-crc   "[stage2] FAIL K"            35  corrupt_loader
case_run REG-0002-manifest     "[stage2] FAIL F"            35  corrupt_manifest
case_run REG-0003-stage2-magic "[stage1] FAIL M"            35  corrupt_stage2
case_run REG-0004-nucleus-crc  "PANIC: nucleus CRC"         35  corrupt_nucleus
case_run REG-0005-unhandled-ud "[trap] #UD at"              35  unhandled_ud
REG_SMP=2 case_run REG-0008-broken-trampoline "smp: cpu with apic id 1 did not start" 35 broken_trampoline
case_run REG-0006-go-panic     "panic: kargs asked for a panic" 35  go_panic
case_run REG-0007-go-nil-fault "[trap] fault in the Go kernel"  35  go_nil_fault
echo "$pass passed, $failn failed"
[ "$failn" -eq 0 ]
