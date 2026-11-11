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
| W14 | Desktop groundwork | M14 | done | [WAVE-14](WAVE-14-desktop-groundwork.md) |
| W15 | Qt on the framebuffer | M15 | done | [WAVE-15](WAVE-15-qt.md) |
| W16 | An X11 desktop | M16 | done | [WAVE-16](WAVE-16-x11-desktop.md) |
| W17 | Networking | M17 | done | [WAVE-17](WAVE-17-networking.md) |
| W18 | Foundations for bigger programs | M18 | done | [WAVE-18](WAVE-18-foundations.md) |
| W19 | A multi-user Unix | M19 | done | [WAVE-19](WAVE-19-unix.md) |
| W20 | Many CPUs, hardened | M20 | done | [WAVE-20](WAVE-20-smp.md) |
| W21 | A firmware framebuffer | M21 | done | [WAVE-21](WAVE-21-framebuffer.md) |
| W22 | Sound and an absolute pointer | M22 | done | [WAVE-22](WAVE-22-sound-tablet.md) |
| W23 | /proc and virtio devices | M23 | done | [WAVE-23](WAVE-23-proc-virtio.md) |
| W24 | ssh, TLS and a browser | M24 | done | [WAVE-24](WAVE-24-ssh-browser.md) |
| W25 | Cheaper round trips | M25 | planned | [WAVE-25](WAVE-25-speed.md) |

**Learning mode for W1-W3.** The starter kit ships working code for these waves. To learn by typing, run `git switch -c learn`, delete the file a slice creates, and rebuild it from the slice steps. `make smoke` tells you when you match the reference behaviour; `git diff main -- <file>` shows the reference.

Every wave ends with the same exit checklist: `make clean test` green · `make sizes` in budget · DEVLOG wave summary · BUGS updated · tag `wave-N`.
