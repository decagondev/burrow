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
  timeout 20 "$QEMU" -machine pc -cpu qemu64 -m 128M \
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
  "$@" "$img"
  boot "$img" "$log"
  if grep -qF -- "$want" "$log" && { [ "$want_status" = any ] || [ "$STATUS" -eq "$want_status" ]; }; then
    echo "  ok    $name"; pass=$((pass+1))
  else
    echo "  FAIL  $name (status=$STATUS, wanted '$want' / $want_status)"; tail -5 "$log"; failn=$((failn+1))
  fi
}

# --- cases -------------------------------------------------------------------
# Loader bytes live at LBA 34. Flip one byte in the middle of its code.
corrupt_loader()   { flip_byte "$1" $((34*512 + 3000)); }
# Manifest magic at LBA 1 offset 0: stage2 must refuse with code F.
corrupt_manifest() { flip_byte "$1" $((1*512)); }
# stage2 magic 'S2' at LBA 2 offset 2: stage1 must refuse with code M.
corrupt_stage2()   { flip_byte "$1" $((2*512 + 2)); }

echo "[regression]"
case_run REG-0001-loader-crc   "[stage2] FAIL K"            35  corrupt_loader
case_run REG-0002-manifest     "[stage2] FAIL F"            35  corrupt_manifest
case_run REG-0003-stage2-magic "[stage1] FAIL M"            35  corrupt_stage2
echo "$pass passed, $failn failed"
[ "$failn" -eq 0 ]
