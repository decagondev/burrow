# DEVLOG

Newest entries at the top. One entry per working session, plus a summary per wave. Write what you did, what surprised you, and what you would tell your past self. Link BUG and ADR ids.

Template:
```text
## YYYY-MM-DD — W?-S?? short title
Did:       ...
Surprise:  ...
Learned:   ...
Next:      ...
Links:     BUG-xxxx, ADR-xxxx
```

---

## 2026-11-02 — Waves 0-3 complete: boot to C loader (M0-M3)

**Did:** Built the repo skeleton, contracts (`boot_info.h`, `manifest.h`, `boot_abi.inc`), `mkimage` with Go tests, stage1, stage2 (A20, E820, manifest, chunked load, CPUID, boot_info, protected mode, CRC verify, identity paging, long mode), and the C loader (kprintf on `char_sink`, serial, memory map, manifest validation, self-CRC, ATA PIO cross-check). Harness: 39 host C checks under ASan/UBSan, Go tests, ordered-marker smoke test with debug-exit, regression suite of mutated images.

**Numbers:** stage1 282/446 bytes, stage2 1502/16384, loader 6024 bytes. Full `make test` under 5 seconds.

**Surprises:**
- First QEMU boot of the full chain passed on the first try, because each contract had been written down before the code.
- The regression suite found a *design* flaw (BUG-0003): a corrupted loader cannot be trusted to check itself. Fixed by verifying in stage2 before executing ("verify before you execute").
- Failure paths that just `hlt` made regression tests wait for a 20 s timeout. Adding debug-exit to every failure path took the suite from 40 s to 0.5 s.

**Learned:** Testing negative paths (broken images) is as valuable as the happy path; it tested our *error handling*, which nothing else exercises.

**Next:** W4 higher-half hand-off. Read MEMORY_MAP §2 first.

## 2026-11-02 — Research and design (pre-W0)

**Did:** Surveyed Biscuit, eggos, gopher-os, TamaGo, the GOOS=none proposal, TinyGo, gVisor, doomgeneric ([RESEARCH.md](../01-research/RESEARCH.md)). Chose: asm → C loader → resident C nucleus → stock Go kernel on a Linux-syscall shim (ADR-0001, ADR-0003).

**Experiment:** Ran a syscall census of a Go 1.22 program (22 distinct syscalls). Discovered the runtime reserves >1 GiB of `PROT_NONE` address space at start-up, passes TLS via `CLONE_SETTLS`, and checks fds 0-2 with `fcntl`. Linking at `-T 0x7e0000000000` works. All three went straight into the design (PIT-05, GO_RUNTIME_HOSTING §4).

**Surprise:** Compile-checking the Doom backend snippet found that `<linux/input.h>` and `doomkeys.h` both define `KEY_ENTER` (BUG-0004).

**Next:** W0.
