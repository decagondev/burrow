# ADR-0008: Raw disk + manifest, filesystem later

- **Status:** Accepted
- **Date:** 2026-11-02
- **Wave:** W0

## Context
Stage2 has ~16 KiB and the BIOS; parsing FAT or ext2 there is a distraction.

## Decision
A one-sector manifest at LBA 1 lists named images (LBA, size, CRC-32). The host tool `mkimage` writes it. Real filesystems arrive in the Go kernel (W8) where they belong.

## Consequences
- Booting needs no filesystem code; integrity checking is built in.
- The disk is not partitioned. A later slice may add an MBR partition entry so other OSes see a sane disk.
