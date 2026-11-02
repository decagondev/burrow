# Test Strategy

Decision: [ADR-0007](../03-decisions/ADR-0007-test-strategy.md). Principle: **most kernel logic should never need a VM to be tested.**

## 1. The pyramid

```mermaid
flowchart TB
    R["Regression (QEMU)<br/>broken images must fail loudly<br/>make regress"]
    S["Smoke (QEMU)<br/>ordered serial markers + exit status<br/>make smoke"]
    K["In-kernel ktest (QEMU, W5+)<br/>nucleus + Go tests that need real hardware"]
    C["Contract tests<br/>struct offsets on both sides of every ABI"]
    U["Unit tests (host, seconds)<br/>C minitest + ASan/UBSan, Go go test"]
    R --> S --> K --> C --> U
```

| Kind | Command | Runs where | Speed | Example |
|---|---|---|---|---|
| Unit (C) | `make test-c` | host gcc, ASan+UBSan | <1 s | `test_elf64.c` |
| Unit (Go) | `make test-go` | host `go test` | ~1 s | UART init sequence via `FakePorts` |
| Contract | inside unit tests + `_Static_assert` | both | — | `TestManifestLayoutMatchesC` |
| Smoke | `make smoke` | QEMU headless | ~0.5 s | `boot_to_loader.txt`, `boot_to_nucleus.txt` |
| Regression | `make regress` | QEMU, mutated images | ~0.5 s | REG-0001 corrupt loader |
| ktest (W5+) | `make ktest` | QEMU, one image per suite | ~0.5 s per suite | `tests/ktest/trap.txt` |
| Go boot (W6+) | `make go-smoke` | QEMU, `build/go.img` | ~10 s | `boot_to_go.txt`: hello, `kernel/selftest`, drivers, then the console typed into by `tests/smoke/keys/boot_to_go.keys` (QEMU monitor `sendkey`, `tests/smoke/sendkeys.py`) until `poweroff` |
| FAT interop (W8+) | `make fat-interop` | host: `fsck.fat`, `mkfs.fat`, mtools | seconds | our FAT32 images vs Linux's tools, both ways; skips if they are missing |
| hostsim (W7+) | `go test ./hostsim` (part of `make test-go`) | host, simulated 8042/16550/IRQ lines | <1 s | the driver stack and console without QEMU; `make hostsim` runs it interactively |
| Census (W6+) | `make census` | host strace | seconds | Go upgrade adds a syscall (`tests/regression/census.sh`) |
| Docs | `make docs-check` | node + mermaid | seconds | all diagrams parse |

`make test` = unit + census + smoke + go-smoke + ktest + regress.

## 2. Rules

1. **Pure core, thin shell.** If a function can take bytes and return a result, it goes in its own file with no `io.h` include, and gets host tests.
2. **Every interface gets a fake.** C: a struct of function pointers (`char_sink`, `block_device`). Go: an interface in `hal` + a fake in `haltest`.
3. **Every bug gets a test** named after it (`REG-NNNN` or a unit test referencing `BUG-NNNN`) unless genuinely untestable; then BUGS says why.
4. **Markers are append-only.** Never weaken an expect file to make a test pass.
5. **Two implementations agreeing is evidence.** ELF tests build images with the host `<elf.h>`; ATA output is compared with BIOS output; mkimage CRC equals stage2 asm CRC equals loader C CRC.
6. **Fail fast and loud.** All failure paths write debug-exit `0x11`, so a failing test finishes in milliseconds, not at a timeout.

## 3. Serial-marker protocol

- Format: `[component] message`; success line `BURROW:<NAME>:OK`.
- `tests/smoke/smoke.sh <img> <expect-file>` asserts every expect line appears **in order**, no line contains `PANIC` or `FAIL`, and QEMU exits with 33.
- Logs land in `build/smoke-<name>.log`; attach them to bug entries.

## 4. QEMU exit codes

| Guest writes to 0xF4 | QEMU process exit status | Meaning |
|---|---|---|
| 0x10 | 33 | pass |
| 0x11 | 35 | fail (any stage) |
| (nothing, timeout) | 124 | hang = bug; investigate with `make debug` |

## 5. Adding a test: checklists

**New pure C module:** create `tests/unit/c/test_<mod>.c`, add the `.c` to `UNIT_SRC` in the Makefile, declare and `RUN()` each test in `test_main.c`.
**New boot stage marker:** add the print, append the line to the expect file, run `make smoke`.
**New regression case:** write a mutator function in `tests/regression/run_all.sh`, add `case_run REG-NNNN-name "<expected text>" 35 mutator`, link it from the BUG entry.
**New ktest (in-kernel test):** write `int kt_<name>(void)` using `KT_CHECK` in a `nucleus/ktest_*.c`, declare it in `nucleus/ktest.h`, add it to the table in `nucleus/ktest.c`, list it in a suite `tests/ktest/<suite>.txt` and append `[ktest] <name> ok` to `tests/ktest/expect/<suite>.txt`. `make ktest` builds `build/ktest-<suite>.img` (the normal image plus a `ktest` payload) and runs the smoke checker on it. A test that must crash goes in the regression suite instead (`with_ktest <name>` in `run_all.sh`, e.g. REG-0005).

**Go kernel behaviour that must crash** (W7+): the image gets a `kargs` payload whose words become the Go kernel's `argv` (`with_kargs <words>` in `run_all.sh`; the words are handled in `kernel/cmd/kernel/kargs.go`), e.g. REG-0006 `panic=boot`, REG-0007 `fault=nil`.

**User processes** (W9+): nucleus mechanism in the ktest suite `user` (ring-3 code assembled by hand in `nucleus/ktest_uthread.c`); Go policy in `kernel/proc` against `proctest.Fake` (inject a request, read the reply); end to end in `make go-smoke` (`run /bin/hello`, `/bin/crash`, `/bin/musl-hello`). A new syscall: add it to `kernel/proc`'s table, a test in `system_test.go`, a row in `POSIX_COVERAGE.md`; compare with Linux by running the same static binary there under `strace`.

**Typing into a boot** (W7+): pass a keys file as the 4th argument of `tests/smoke/smoke.sh`. Steps: `wait <text>` (in the serial log), `type <text>`, `key <qemu-key-name>`. The guest's output, not the keys file, is what the expect file checks.

**New Go driver:** a package under `kernel/drivers/` that calls `driver.Register` from `init()` and touches hardware only through `driver.Env`; host-test it with `haltest` fakes (`FakePorts`, `FakeIRQ`, `Fake8042`), add it to `kernel/hostsim` if the console can reach it, and import it for side effects in `kernel/cmd/kernel/main.go`.
**New Go package:** `_test.go` next to it; use `haltest` fakes; run `make test-go`.

## 6. Diagram checking

`tools/doccheck/` contains a Node script that parses every Mermaid block with the official parser:

```sh
cd tools/doccheck && npm install && node check.mjs $(git ls-files '*.md' | sed 's#^#../../#')
```
