# W0 Foundations  (M0)  — status: done

Goal: before any boot code exists, the repo can build, test and document itself.

### [x] W0-S01 Repository skeleton (E00.F01.S01)
Files: `README.md`, `.gitignore`, `.editorconfig`, folders `boot/ include/ loader/ kernel/ tools/ tests/ scripts/ docs/`.
Test: `git status` clean after `make clean`.

### [x] W0-S02 Toolchain check (E00.F01.S01)
Follow `docs/06-guides/TOOLCHAIN_SETUP.md`. Test: `nasm -v`, `gcc --version`, `go version`, `qemu-system-x86_64 --version` all succeed.

### [x] W0-S03 Makefile with `all/run/debug/test/clean` (E00.F01.S01)
Learn: pattern rules, order-only prerequisites (`| $(BUILD)`).

### [x] W0-S04 Contract headers first (E03.F01)
Files: `include/burrow/boot_info.h`, `manifest.h`, `boot/boot_abi.inc`. Write the *data* before the code that produces it. Test: compiles; `_Static_assert`s hold.

### [x] W0-S05 `mkimage` host tool with tests (E00.F01)
Files: `tools/mkimage/{manifest,image,main}.go`, `mkimage_test.go`. Test: `make test-go`. Learn: pure core (`BuildImage`) vs I/O shell (`main`).

### [x] W0-S06 Host unit-test runner (E00.F02.S01)
Files: `tests/unit/c/minitest.h`, `test_main.c`. Test: `make test-c` prints `0 failures`. Learn: ASan/UBSan catch memory bugs your kernel code would hide.

### [x] W0-S07 Smoke and regression harness (E00.F02.S02-S03)
Files: `tests/smoke/smoke.sh`, `tests/regression/run_all.sh`. Learn: QEMU `isa-debug-exit` (ADR-0007).

### [x] W0-S08 Living docs (E00.F03.S01)
Files: everything under `docs/`. Test: Mermaid diagrams parse (see TEST_STRATEGY.md).
