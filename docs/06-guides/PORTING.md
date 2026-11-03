# Porting software to Burrow

Burrow runs **static Linux x86-64 programs unchanged** ([ADR-0006](../03-decisions/ADR-0006-linux-abi-userland.md)): the Go kernel answers their Linux system calls. Porting is therefore mostly *building* (static, for x86-64 Linux) and, when a program needs a call Burrow lacks, *adding that call's Linux semantics* to `kernel/proc`. This guide is written as ports land (W10-W12); [WORKBENCH.md](WORKBENCH.md) is about using what is here; [POSIX_COVERAGE.md](../02-architecture/POSIX_COVERAGE.md) is the list of what exists.

## The recipe

1. **Build static for Linux.** C: `musl-gcc -static` (the Makefile builds every `user/*.c` this way; `user/doom/` and busybox show bigger builds driven by a script). Go: `GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build` (every `user/go/<name>/main.go` becomes `/bin/<name>`). Programs that include Linux headers (`<linux/fb.h>`, `<linux/input.h>`) get them from `build/uapi`, which links to the host's kernel UAPI headers and nothing of glibc.
2. **Put it in the initrd** (`/bin` comes from `user/`), or, if it needs a download or is big, on its own image the way `go-busybox.img` and `go-doom.img` are made: the usual initrd plus the program, data on a FAT disk at `/mnt`.
3. **Run it from the console** (`run /bin/prog args`) and read the serial log. An unknown call prints `[proc] WARN: pid N (prog): unimplemented syscall NR` once; a crash prints `killed: page fault at ... (rip ...)`.
4. **Implement what is missing**, with Linux's semantics: the man page, then what the program actually relies on. Add a host test in `kernel/proc` against `proctest.Fake` and a row in POSIX_COVERAGE.md. Compare with Linux by running the same static binary there under `strace -f`.
5. **Keep it running**: a keys file and an expect file under `tests/smoke/` (the port prints a line that says it worked), wired into `make test`; anything downloaded is pinned by sha256 and the check skips without it.

## What ports have needed so far

| Port | Missing piece it found | Wave |
|---|---|---|
| musl C programs | nothing beyond W9's 25 calls | W9 |
| our shell and tools | fork, exec, pipes, signals, a TTY | W10 |
| busybox ash | `poll` (its line editor), `prctl` | W10 |
| Doom | `readv` (musl's `fread`), then only devices: fbdev, evdev | W11 |
| musl pthreads | threads (`clone`, `futex`, per-thread signals), `mprotect` for thread stacks | W12 |
| a Go program | `epoll`, `eventfd`, `sigaltstack` + `SA_ONSTACK`, `madvise`, rlimits, and demand-zero `mmap` (the runtime reserves hundreds of MiB it never touches) | W12 |
| Lua 5.4 | nothing | W12 |
| C++ (libstdc++: exceptions, `std::thread`) | nothing beyond the threads above | W12 |
| TinyCC, compiling on Burrow | nothing in the kernel; its last release mislinks modern musl objects, so a newer snapshot | W12 |

## Toolchains

- **C:** `musl-gcc` (Debian/Ubuntu `musl-tools`) for programs in the tree; `scripts/ports.sh` uses musl.cc's `x86_64-linux-musl-cross` for C and C++ (it carries a complete musl sysroot and libstdc++).
- **C++:** that toolchain's `x86_64-linux-musl-g++ -static`. Exceptions and threads work unchanged.
- **Go:** the host's Go, `GOOS=linux CGO_ENABLED=0`.
- **On Burrow:** `tcc -static` with the musl headers and `libc.a` under `/usr` (`make ports`). Use `-static`: Burrow has no dynamic loader. `tcc -run` is not available in a static `tcc`.
- **When a ported binary crashes, run the same binary on Linux first.** A crash at the same address on both is the program's (or its toolchain's) bug; TinyCC 0.9.27 was such a case. Burrow aims to fail exactly where Linux fails.

## Things that bite

- **Implement the contract, not the call.** The Go runtime needs `MADV_DONTNEED` to zero (BUG-0009), lazy `mmap` reservations, signals on the alternate stack, and `FUTEX_WAIT` to fail with `EAGAIN` when the word changed. A call that returns 0 but does not do what Linux does fails later and far away.
- **Signals and threads.** Masks and alternate stacks are per thread; `tgkill` targets one thread (Go preempts goroutines that way). A process-directed signal goes to the first thread that does not block it.
- **Headers from two worlds.** `<linux/input.h>` and Doom's `doomkeys.h` both define `KEY_ENTER` (BUG-0004): capture one before including the other.
- **A Windows-backed checkout** cannot hold files whose names differ only in case (the Linux UAPI headers have such pairs): link to such trees instead of copying them.
- **Smoke waits** match the serial log, which also holds the echo of what was typed: wait for output the command line does not contain.
