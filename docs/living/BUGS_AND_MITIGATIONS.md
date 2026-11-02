# Bugs and Mitigations

Every bug gets an entry, even five-minute ones: the pattern across entries is the lesson. IDs are never reused. Newest at the top of each section.

Template:
```text
## BUG-NNNN: one-line title
- Found:      date, wave/slice, how (test name, symptom)
- Severity:   blocker | major | minor | latent (found before it bit)
- Symptom:    what you saw (paste the log line)
- Root cause: why it happened
- Fix:        what changed (files)
- Mitigation: what stops the CLASS of bug from returning (test, flag, rule)
- Regression: test id, or why none is possible
- Lesson:     one sentence
```

```mermaid
flowchart LR
    A["Symptom seen"] --> B["Open BUG entry<br/>symptom + log"]
    B --> C["Reproduce reliably"]
    C --> D["Write failing test<br/>REG-NNNN or unit"]
    D --> E["Find root cause"]
    E --> F["Fix"]
    F --> G["Test passes,<br/>make test green"]
    G --> H["Mitigation for the class<br/>flag / rule / doc"]
    H --> I["Close entry, DEVLOG line"]
```

---

## Open

*(none)*

## Closed

### BUG-0007: A thread woken before it switched out kept running as T_RUNNABLE
- Found: 2026-11-02, W5-S11, reading `schedule()` while designing `futex_wait` (before any test hit it).
- Severity: latent (would have let pick_next hand the running thread to itself again and confused join/wake logic).
- Symptom: none yet. Path: thread marks itself T_BLOCKED, a waker (or the tick) sets it T_RUNNABLE before `schedule()` runs, `pick_next` returns the thread itself, `schedule()` returned early without restoring T_RUNNING.
- Root cause: the `next == prev` early return assumed prev was still T_RUNNING.
- Fix: `nucleus/sched.c` sets `prev->state = T_RUNNING` on that path.
- Mitigation: blocking primitives go through `thread_block_unlock`, which marks the thread blocked before the lock drops, so wake-before-switch is a designed-in case, not an accident (PIT-23).
- Regression: ktest `futex_ping_pong` (20,000 wake/wait pairs) exercises the path continuously.
- Lesson: every early return in a state machine must leave the state consistent.

### BUG-0006: CR0.WP was off, so ring 0 ignored read-only pages (W4's W^X was not enforced)
- Found: 2026-11-02, W5-S07, writing the `vmm_kernel_wx` ktest.
- Severity: latent major: the loader mapped nucleus text read-only, but with CR0.WP=0 the CPU lets supervisor code write to any present page.
- Symptom: none yet; would have shown as a stray kernel write silently patching nucleus code.
- Root cause: neither stage2 nor the loader sets CR0.WP; it is off after reset and only matters for ring 0.
- Fix: `vmm_init` sets CR0.WP (`nucleus/vmm.c`).
- Mitigation: ktests that try the forbidden access (`vmm_kernel_wx`, `vmm_protect_readonly`) instead of only reading back the flags; a table walk proved the flags were right all along.
- Regression: ktest `vmm_kernel_wx` (write to nucleus text must #PF with P=1).
- Lesson: check that a protection is enforced, not only that it is configured.

### BUG-0005: A regression case whose mutator failed was silently skipped
- Found: 2026-11-02, W4-S03, while adding REG-0004 (its `lba_of` helper failed on its first version).
- Severity: major (test harness): a broken case vanished from the count instead of failing.
- Symptom: `make regress` printed `3 passed, 0 failed` with no line at all for REG-0004.
- Root cause: `case_run` ran the mutator as a bare command; its failure aborted the case without touching `pass`/`failn`, and nothing compared the number of cases run with the number declared.
- Fix: `tests/regression/run_all.sh` treats a failing mutator as `FAIL <name> (mutator ... failed)`; mutators that compute offsets return non-zero on error.
- Mitigation: rule: a test harness must report every case it was asked to run. Read the per-case lines, not only the totals.
- Regression: none automated (a deliberately broken mutator would make `make regress` red); the REG-0004 line itself is the visible check.
- Lesson: test the tester; a total without the per-case lines hides missing cases.

### BUG-0004: `KEY_ENTER` defined by both `<linux/input.h>` and `doomkeys.h`
- Found: 2026-11-02, while compile-checking `snippets/userland/doomgeneric_burrow.c` (`-Werror`).
- Severity: latent (would have shipped as "Enter key does nothing / acts as '='").
- Symptom: macro redefinition warning; `case KEY_ENTER` would compare the evdev code against Doom's value 13 (`KEY_EQUAL` in evdev).
- Root cause: two headers from different worlds share a name with different meanings (evdev 28, Doom 13).
- Fix: capture `EVDEV_KEY_ENTER = KEY_ENTER` before including `doomkeys.h`, `#undef`, map explicitly.
- Mitigation: build ported code with `-Werror`; when bridging two APIs, prefix constants from one side.
- Regression: none possible until W11; add an evdev→doom key-map unit test in E10.F02.
- Lesson: compile-check reference code, don't just read it.

### BUG-0003: A corrupted loader failed for the wrong reason (cannot self-verify)
- Found: 2026-11-02, W3, regression case REG-0001 (flip one byte in the loader image).
- Severity: major (design flaw).
- Symptom: expected `PANIC: loader CRC mismatch`; got `PANIC: need at least 32 MiB of RAM`. The flipped byte had changed code/data used *before* the self-check.
- Root cause: a program that might be corrupted cannot be relied on to detect its own corruption; the check ran inside the thing being checked.
- Fix: stage2 computes CRC-32 of the loader (32-bit asm `crc32_ieee`) and compares with the manifest **before** jumping (`[stage2] FAIL K`). Loader self-check kept as defence in depth.
- Mitigation: rule "verify before you execute" for every image the loader/nucleus loads (W4-S03, W6-S02). All failure paths also write debug-exit so tests fail in milliseconds.
- Regression: REG-0001-loader-crc.
- Lesson: negative tests exercise the code that matters most when things go wrong.

### BUG-0002: ELF unit test fixture smaller than the segment it described
- Found: 2026-11-02, W3-S10, `elf_load_segment` failed under `make test-c`.
- Severity: minor (test bug, parser correct).
- Symptom: `s.memsz == 0x0, want 0x2000`.
- Root cause: segment data at file offset 0x1000 in a 4096-byte buffer, so `offset + filesz > len`; the parser correctly rejected it.
- Fix: buffer 8192 bytes, with a comment pointing here.
- Mitigation: when a new test fails, first ask "is the test right?"; fixtures state their size assumptions in comments.
- Regression: the test itself.
- Lesson: the bounds check worked exactly as designed: good sign for untrusted ELF input.

### BUG-0001: `noreturn` functions that "return"
- Found: 2026-11-02, W3-S11, first `make` with `-Werror`.
- Severity: minor (build break).
- Symptom: `'noreturn' function does return` for `panic` and `loader_main`.
- Root cause: they ended in `cpu_halt_forever()`, which was not itself marked `noreturn`, so GCC could not prove the end was unreachable.
- Fix: `__attribute__((noreturn))` on `cpu_halt_forever` in `loader/io.h`.
- Mitigation: keep `-Werror`; mark halt/panic/exit helpers `noreturn` everywhere (nucleus too).
- Regression: the build itself.
- Lesson: attributes are contracts the compiler can only check if they are complete down the call chain.

---

## Anticipated pitfalls (known before they bite)

Pre-registered hazards with planned mitigations. If one bites anyway, open a BUG and link the PIT id. Full list for Go hosting: [GO_RUNTIME_HOSTING.md §5](../02-architecture/GO_RUNTIME_HOSTING.md) (PIT-01..PIT-11).

| ID | Wave | Pitfall | Planned mitigation |
|---|---|---|---|
| PIT-20 | W4 | Enabling NX bit 63 in PTEs without EFER.NXE → #GP/#PF | set EFER.NXE before CR3 load; unit test flags |
| PIT-21 | W4 | Identity map removed while still executing low code | remove only after jumping to higher half |
| PIT-22 | W5 | Context switch forgets FS base or TSS.RSP0 | `switch_to` checklist + ktest |
| PIT-23 | W5 | Lost wake-up in futex (check and sleep not atomic) | hold the bucket lock across compare + enqueue |
| PIT-24 | W9 | SYSRET with non-canonical RCX (#GP in ring 0) | canonical check, IRETQ fallback (in gate snippet) |
| PIT-25 | W9 | Kernel follows a user pointer into kernel memory | copyin/copyout validate ranges against user VMAs |
| PIT-26 | W10 | fork copies the Go kernel mappings as user-visible | kernel ranges always supervisor-only; ktest |
| PIT-27 | W11 | Framebuffer pitch != width*4 | always use `line_length` (done in the Doom snippet) |
