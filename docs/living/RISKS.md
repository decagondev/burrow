# Risk Register

Score = Likelihood x Impact (1-3 each). Review at every wave exit.

| ID | Risk | L | I | Score | Mitigation | Trigger / owner action |
|---|---|---|---|---|---|---|
| R-01 | Stock Go runtime needs behaviour the shim cannot provide in ring 0 | 2 | 3 | 6 | W6-S01 spike first; census-on-failure; plan B TamaGo (ADR-0003) | > 2 waves stuck in W6 → plan B review |
| R-02 | Go version upgrade changes runtime syscalls | 3 | 2 | 6 | pin toolchain; census regression test | census diff in CI |
| R-03 | Go heap exhaustion is unrecoverable | 2 | 3 | 6 | `GOMEMLIMIT`, heap budget from E820, per-operation reservations later (Biscuit) | OOM panic in ktest |
| R-04 | Address-layout assumptions wrong (Go hints vs user ranges) | 2 | 2 | 4 | spike; static-PIE fallback (MEMORY_MAP §2) | spike result |
| R-05 | Scope creep before M6 | 3 | 2 | 6 | roadmap critical path; story-level only beyond W6 | any W7+ work before M6 |
| R-06 | Real-hardware differences: firmware quirks, and devices QEMU's `pc` machine does not have | 3 | 2 | 6 | W26 added the UEFI path (ADR-0013): a GPT stick image (`make uefi-usb`) whose loader and nucleus write on GOP's screen, and REAL_HARDWARE.md asking testers for a photo; OVMF is the only UEFI firmware it has met. Re-scored at W26's exit: likelihood 3 (a PC built in the last decade lacks the PS/2, IDE and e1000 Burrow drives), impact 2 (the boot reaches the Go console and can do nothing there). Next: W27's drivers (the I/O APIC and MSI, AHCI, NVMe, xHCI with USB keyboards and storage) and a first-boot report | the first tester's photo; W27's exit |
| R-07 | Syscall-ring latency makes Doom slow | 1 | 2 | 2 | measured in W11: `make doom-bench` = 100-104 fps (timedemo demo1, QEMU TCG); retired unless the bench drops | FPS < 20 |
| R-08 | Learning fatigue on long debugging sessions | 2 | 2 | 4 | 30-120 min slices; DEVLOG; celebrate milestones | three sessions without a commit |
| R-09 | A blocking call's round trip through Burrow's dispatcher limits ssh: `scp` at about 1 MB/s on TCG where TCP moves 5.5 (W24-S06) | 2 | 1 | 2 | W25 (ADR-0012) halved a call that cannot block (getpid 103 to 52 us: the vDSO for the Go kernel, calls answered on the dispatcher, reply and wait in one crossing) and cut clock reads to 85 ns; `scp` 1.25 MB/s, NetSurf's first page 3.5 to 2.8 s. A blocking call still pays a goroutine hand-off (a pipe write 118 us from taken to answered), and answering such calls on the dispatcher strands the goroutines they wake (W25-S04). Re-scored at W25's exit: likelihood 2, impact 1 (only TCG's throughput; nothing fails). Next: a dispatcher that hands off its P while it waits in the nucleus, then blocking calls answered on it when they need not wait | `ssh-bench` under 1.0 MB/s; `lat-bench` over its budgets |
