#!/usr/bin/env bash
# census-ssh.sh - W24-S02: which system calls does dropbear make for a
# session? Runs build/net-root's dropbear on the host as the calling user
# (a high port, its own host key, an authorized_keys directory given with
# -D, so nothing in ~/.ssh is touched) under strace -f, logs in with the
# host's OpenSSH client and a throwaway key, runs a command and an
# interactive session on a pseudo-terminal, and compares the names with
# tests/regression/census-ssh.txt, as census-net.sh does for curl.
# Run as a normal user, dropbear never changes ids; on Burrow it runs as
# root and adds setgroups, setgid and setuid, which the file lists by hand.
#
# Usage: tests/regression/census-ssh.sh [build-dir]
set -euo pipefail
export LC_ALL=C # comm and sort agree with the filter's order
B="${1:-build}"
NET="$B/net-root"
EXPECT="tests/regression/census-ssh.txt"
command -v strace >/dev/null || { echo "SSH CENSUS FAIL: strace is not installed"; exit 1; }
command -v ssh >/dev/null || { echo "SSH CENSUS SKIP (no ssh client on the host)"; exit 0; }
[ -x "$NET/bin/dropbear" ] || { echo "SSH CENSUS SKIP (make net-ports builds dropbear)"; exit 0; }

mkdir -p "${NET_CACHE:-$HOME/.cache/burrow-net}"
D=$(mktemp -d "${NET_CACHE:-$HOME/.cache/burrow-net}/census-ssh.XXXXXX") # dropbear checks the modes of every directory up to /
mkdir -p "$D/auth"
"$NET/bin/dropbearkey" -t ed25519 -f "$D/hostkey" > /dev/null
ssh-keygen -q -t ed25519 -N '' -f "$D/key"
cp "$D/key.pub" "$D/auth/authorized_keys" && chmod 700 "$D/auth" && chmod 600 "$D/auth/authorized_keys"
PORT=$(python3 -c 'import socket; s = socket.socket(); s.bind(("127.0.0.1", 0)); print(s.getsockname()[1])')
strace -f -qq -o "$D/dropbear.strace" "$NET/bin/dropbear" -F -E -s -r "$D/hostkey" -p "127.0.0.1:$PORT" -D "$D/auth" \
  > "$D/dropbear.log" 2>&1 &
SERVER=$!
trap 'kill $SERVER 2>/dev/null || true; cp "$D"/*.out "$D"/*.log "$B/" 2>/dev/null || true; rm -rf "$D"' EXIT
for _ in $(seq 100); do python3 -c "import socket; socket.create_connection(('127.0.0.1', $PORT))" 2>/dev/null && break; sleep 0.1; done
SSH=(timeout 60 ssh -i "$D/key" -p "$PORT" -o StrictHostKeyChecking=no -o UserKnownHostsFile="$D/known" -o BatchMode=yes)
"${SSH[@]}" "$USER@127.0.0.1" 'id -u; echo census: command ran' > "$D/command.out" 2>&1 ||
  { echo "SSH CENSUS FAIL: the command session failed:"; cat "$D/command.out" "$D/dropbear.log"; exit 1; }
grep -q 'census: command ran' "$D/command.out" || { echo "SSH CENSUS FAIL: no command output"; exit 1; }
printf 'stty size\nexit 3\n' | "${SSH[@]}" -tt "$USER@127.0.0.1" > "$D/pty.out" 2>&1 && status=0 || status=$?
# On WSL's kernel a non-root dropbear's session child is hung up as it
# opens its pty (SIGHUP, before the shell runs): the calls up to there are
# still the census's, and Burrow's own ssh-smoke checks the session works.
[ "$status" = 3 ] || echo "ssh census: the pty session ended with $status on this host (counted anyway)"
kill $SERVER 2>/dev/null || true
for _ in $(seq 50); do kill -0 $SERVER 2>/dev/null || break; sleep 0.1; done # strace writes its last lines

cp "$D/dropbear.strace" "$B/census-ssh.strace"
# Only dropbear's own calls: a process stops counting once it runs another
# program (the user's shell, glibc's on the host), and so do its children.
python3 - "$B/census-ssh.strace" > "$B/census-ssh.seen" <<'PY'
import re, sys
other, names = set(), set()
for line in open(sys.argv[1], errors="replace"):
    start = re.match(r"(\d+) +([a-z_][a-z0-9_]*)\((.*)", line)
    resumed = re.match(r"(\d+) +<\.\.\. ([a-z_][a-z0-9_]*) resumed>(.*)", line)
    m = start or resumed
    if not m:
        continue
    pid, name, rest = m.groups()
    if pid in other:  # a shell, or a child of one: follow its forks
        child = re.search(r"= (\d+)\s*$", rest)
        if name in ("clone", "clone3", "fork", "vfork") and child:
            other.add(child.group(1))
        continue
    # dropbear running itself again (execveat of its own descriptor, its
    # arguments naming it) is still dropbear; any other program is not
    if start and name in ("execve", "execveat") and "dropbear" not in rest:
        other.add(pid)
        continue
    if start and name != "execve":
        names.add(name)
print("\n".join(sorted(names)))
PY
grep -v '^#' "$EXPECT" | awk 'NF {print $1}' | sort -u > "$B/census-ssh.expected"
new=$(comm -23 "$B/census-ssh.seen" "$B/census-ssh.expected")
gone=$(comm -13 "$B/census-ssh.seen" "$B/census-ssh.expected")
if [ -n "$gone" ]; then echo "ssh census: no longer used: $(echo $gone)"; fi
if [ -n "$new" ]; then
  echo "SSH CENSUS FAIL: new syscalls: $(echo $new)"
  echo "  add them to $EXPECT with their numbers (and implement them or name the slice)"
  exit 1
fi
echo "SSH CENSUS PASS ($(wc -l < "$B/census-ssh.seen") syscalls; kernel/proc's TestCensusSsh checks each is served)"
