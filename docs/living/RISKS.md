# Risk Register

Score = Likelihood x Impact (1-3 each). Review at every wave exit.

| ID | Risk | L | I | Score | Mitigation | Trigger / owner action |
|---|---|---|---|---|---|---|
| R-01 | Stock Go runtime needs behaviour the shim cannot provide in ring 0 | 2 | 3 | 6 | W6-S01 spike first; census-on-failure; plan B TamaGo (ADR-0003) | > 2 waves stuck in W6 → plan B review |
| R-02 | Go version upgrade changes runtime syscalls | 3 | 2 | 6 | pin toolchain; census regression test | census diff in CI |
| R-03 | Go heap exhaustion is unrecoverable | 2 | 3 | 6 | `GOMEMLIMIT`, heap budget from E820, per-operation reservations later (Biscuit) | OOM panic in ktest |
| R-04 | Address-layout assumptions wrong (Go hints vs user ranges) | 2 | 2 | 4 | spike; static-PIE fallback (MEMORY_MAP §2) | spike result |
| R-05 | Scope creep before M6 | 3 | 2 | 6 | roadmap critical path; story-level only beyond W6 | any W7+ work before M6 |
| R-06 | Real-hardware differences (BIOS quirks) | 2 | 1 | 2 | QEMU first; Bochs as second emulator; UEFI path later | first hardware test |
| R-07 | Syscall-ring latency makes Doom slow | 1 | 2 | 2 | measured in W11: `make doom-bench` = 100-104 fps (timedemo demo1, QEMU TCG); retired unless the bench drops | FPS < 20 |
| R-08 | Learning fatigue on long debugging sessions | 2 | 2 | 4 | 30-120 min slices; DEVLOG; celebrate milestones | three sessions without a commit |
| R-09 | A blocking call's round trip through Burrow's dispatcher limits ssh: `scp` at about 1 MB/s on TCG where TCP moves 5.5 (W24-S06) | 2 | 1 | 2 | measured: not the cipher, not dropbear's window; next: a pipe and socket ping-pong to time a wake-up, then shorten the path | `ssh-bench` under its budget (0.8 MB/s) |
