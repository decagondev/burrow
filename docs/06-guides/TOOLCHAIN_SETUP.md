# Toolchain Setup

| Tool | Version used | Why |
|---|---|---|
| NASM | 2.16+ | boot assembly (`-f bin`) and `-f elf64` objects |
| GCC + binutils | 13+ (x86-64 host) or `x86_64-elf-gcc` cross | freestanding C |
| Go | 1.22+ for host tools; **1.27.x pinned** for the kernel (W6) | tools, kernel |
| QEMU | 8.x+ (`qemu-system-x86_64`) | emulator |
| GDB | any with x86-64 | debugging |
| Python 3 | any | small scripts |
| Node.js | 18+ (optional) | Mermaid check |
| strace | any (optional, Linux) | Go census |

## Ubuntu / Debian / WSL2

```sh
sudo apt install build-essential nasm qemu-system-x86 gdb python3 strace git
# Go: use the official tarball for the pinned version (distro Go is often old)
curl -LO https://go.dev/dl/go1.27.X.linux-amd64.tar.gz   # pick the latest 1.27.x
sudo rm -rf /usr/local/go && sudo tar -C /usr/local -xzf go1.27.X.linux-amd64.tar.gz
export PATH=$PATH:/usr/local/go/bin
```

## macOS

```sh
brew install nasm qemu x86_64-elf-gcc x86_64-elf-binutils x86_64-elf-gdb go python
make CC_TARGET=x86_64-elf-gcc LD_TARGET=x86_64-elf-ld OBJCOPY=x86_64-elf-objcopy
```
The host unit tests use your normal `gcc`/`clang` (`HOSTCC=clang` works; `<elf.h>` is not on macOS, so install `libelf` headers or skip `test_elf64.c` via `UNIT_SRC`).

## Verify

```sh
make clean test     # expect: 0 failures, SMOKE PASS, 3 passed 0 failed
make run            # Ctrl-A then X to quit QEMU
```

## Why the system gcc works for the loader

On x86-64 Linux the system compiler targets the same CPU and ELF format. Freestanding flags (`-ffreestanding -nostdlib -static -fno-pic`) stop it from assuming Linux or glibc. A cross compiler is cleaner and is required on macOS/Windows; switch any time with `CC_TARGET`.
