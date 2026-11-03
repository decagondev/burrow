# Waves

| Wave | Title | Milestone | Status | File |
|---|---|---|---|---|
| W0 | Foundations | M0 | done | [WAVE-00](WAVE-00-foundations.md) |
| W1 | Boot sector | M1 | done | [WAVE-01](WAVE-01-stage1.md) |
| W2 | Stage2 to long mode | M2 | done | [WAVE-02](WAVE-02-stage2.md) |
| W3 | C loader | M3 | done | [WAVE-03](WAVE-03-c-loader.md) |
| W4 | Higher-half hand-off | M4 | done | [WAVE-04](WAVE-04-higher-half.md) |
| W5 | Nucleus core | M5 | done | [WAVE-05](WAVE-05-nucleus.md) |
| W6 | Go says hello | M6 | done | [WAVE-06](WAVE-06-go-runtime.md) |
| W7 | Go drives hardware | M7 | done | [WAVE-07](WAVE-07-go-drivers.md) |
| W8 | Files and pixels | M8 | done | [WAVE-08](WAVE-08-files-pixels.md) |
| W9 | First process | M9 | done | [WAVE-09](WAVE-09-first-process.md) |
| W10 | A Unix | M10 | done | [WAVE-10](WAVE-10-a-unix.md) |
| W11 | DOOM | M11 | done | [WAVE-11](WAVE-11-doom.md) |
| W12 | Ports | M12 | done | [WAVE-12](WAVE-12-ports.md) |
| W13 | A workbench | M13 | done | [WAVE-13](WAVE-13-workbench.md) |
| W14 | Desktop groundwork | M14 | in progress | [WAVE-14](WAVE-14-desktop-groundwork.md) |
| W15 | Qt on the framebuffer | M15 | planned | [WAVE-15](WAVE-15-qt.md) |

**Learning mode for W1-W3.** The starter kit ships working code for these waves. To learn by typing, run `git switch -c learn`, delete the file a slice creates, and rebuild it from the slice steps. `make smoke` tells you when you match the reference behaviour; `git diff main -- <file>` shows the reference.

Every wave ends with the same exit checklist: `make clean test` green · `make sizes` in budget · DEVLOG wave summary · BUGS updated · tag `wave-N`.
