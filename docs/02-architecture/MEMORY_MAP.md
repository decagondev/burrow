# Memory Map

Two maps matter: the **physical** layout while booting (fixed addresses, chosen by us) and the **virtual** layout once the nucleus runs (chosen by us, enforced by page tables). Every fixed address in this file has exactly one definition in code, listed in the "Defined in" column. If you move something, update both.

## 1. Physical memory during boot (Waves 1-3, implemented)

| Range | Size | Contents | Defined in |
|---|---|---|---|
| `0x00000 - 0x004FF` | 1.25 KiB | Real-mode IVT + BIOS data area. **Do not touch.** | BIOS |
| `0x00500 - 0x005FF` | 256 B | A20 test scratch byte at `0x500` | `stage2.asm a20_check` |
| `0x00600 - 0x007FF` | 512 B | Manifest copy (disk LBA 1) | `MANIFEST_ADDR` |
| `0x01000 - 0x0107F` | 128 B | `struct boot_info` | `BOOTINFO_ADDR` |
| `0x02000 - 0x02BFF` | 3 KiB | E820 table, up to 128 x 24 B | `E820_ADDR` |
| `0x03000 - 0x05FFF` | 12 KiB | Boot page tables: PML4, PDPT, PD (identity 1 GiB) | `PML4_ADDR` etc. |
| `0x06000 - 0x07BFF` | ~7 KiB | Real/protected-mode stack (grows down from `0x7C00`) | `stage1.asm` |
| `0x07C00 - 0x07DFF` | 512 B | stage1 | BIOS convention |
| `0x07E00 - 0x0BDFF` | 16 KiB | stage2 slot | `STAGE2_ADDR` |
| `0x10000 - 0x4FFFF` | 256 KiB max | loader image + .bss | `LOADER_ADDR`, `LOADER_MAX_SECT` |
| `0x50000 - 0x7FFFF` | 192 KiB | loader stack (top `0x80000`) | `LOADER_STACK_TOP` |
| `0x80000 - 0x9FBFF` | ~127 KiB | free (check E820) | — |
| `0x9FC00 - 0xFFFFF` | — | EBDA, VGA memory, option ROMs, BIOS. **Reserved.** | hardware |
| `0x100000 -` | — | free RAM per E820; nucleus will load at 1 MiB (W4) | `nucleus/linker.ld` |

```mermaid
flowchart TB
    subgraph low["First MiB (physical)"]
        direction TB
        a["0x00000 IVT + BDA"]
        b["0x00600 manifest copy"]
        c["0x01000 boot_info"]
        d["0x02000 E820 table"]
        e["0x03000 boot page tables"]
        f["0x07C00 stage1"]
        g["0x07E00 stage2 (16 KiB)"]
        h["0x10000 loader (up to 256 KiB)"]
        i["0x80000 loader stack top"]
        j["0x9FC00 EBDA / VGA / BIOS - reserved"]
        a --> b --> c --> d --> e --> f --> g --> h --> i --> j
    end
    j --> k["0x100000+ RAM (E820 usable): nucleus, Go kernel, page frames"]
```

Rule of thumb from experience: **anything below 1 MiB that you did not put there yourself belongs to the firmware.** Always consult the E820 map rather than assuming a hole is free.

## 2. Virtual memory once the nucleus runs (Waves 4-9) *(provisional)*

x86-64 has a 48-bit canonical address space: a lower half `0x0000_0000_0000_0000 - 0x0000_7FFF_FFFF_FFFF` and an upper half `0xFFFF_8000_0000_0000 - 0xFFFF_FFFF_FFFF_FFFF`. Everything in between is non-canonical and faults.

| Range | Owner | Contents | Page flags |
|---|---|---|---|
| `0x0000_0000_0000_0000 - 0x0000_0000_003F_FFFF` | — | unmapped guard: catches NULL derefs | none |
| `0x0000_0000_0040_0000 - 0x0000_00BF_FFFF_FFFF` | user | static ELF images (`0x400000`), brk heap, mmap | U |
| `0x0000_00C0_0000_0000 - 0x0000_00FF_FFFF_FFFF` | Go kernel | Go heap arenas (runtime hint `0xc000000000`) | S |
| `0x0000_0100_0000_0000 - 0x0000_7CFF_FFFF_FFFF` | user | more mmap space | U |
| `0x0000_7D00_0000_0000 - 0x0000_7DFF_FFFF_FFFF` | Go kernel | runtime `PROT_NONE` reservations, thread stacks | S |
| `0x0000_7E00_0000_0000 - 0x0000_7EFF_FFFF_FFFF` | Go kernel | Go kernel image (`-ldflags -T 0x7e0000000000`) | S |
| `0x0000_7F00_0000_0000 - 0x0000_7FFF_FFFF_FFFF` | user | user stacks (top-down) | U |
| `0xFFFF_8000_0000_0000 - +RAM size` | nucleus | **physmap**: all physical RAM mapped linearly | S, NX |
| `0xFFFF_FFFF_8000_0000 - ...` | nucleus | nucleus image (`-mcmodel=kernel`, loaded at phys 1 MiB) | S |

S = supervisor-only, U = user accessible. The Go kernel and nucleus regions are present in **every** address space but marked supervisor-only, so a user process cannot read them, and its SYSCALL return RIP can never point into Go kernel text (this is what makes the gate's caller check sound, [ADR-0004](../03-decisions/ADR-0004-syscall-gate.md)).

```mermaid
flowchart TB
    subgraph lower["Lower half (per address space)"]
        direction TB
        g0["0x0 guard"]
        u1["0x400000 USER images, brk, mmap"]
        gh["0xC0_0000_0000 GO HEAP (supervisor)"]
        u2["0x100_0000_0000 USER mmap"]
        gr["0x7D00_0000_0000 GO reservations (supervisor)"]
        gt["0x7E00_0000_0000 GO KERNEL TEXT (supervisor)"]
        us["0x7F00_0000_0000 USER stacks"]
        g0 --> u1 --> gh --> u2 --> gr --> gt --> us
    end
    subgraph upper["Upper half (shared by all)"]
        direction TB
        pm["0xFFFF_8000_0000_0000 physmap"]
        nu["0xFFFF_FFFF_8000_0000 nucleus"]
        pm --> nu
    end
    lower --> upper
```

### What is already validated

A host experiment on Go 1.22 (see [census](../01-research/GO_RUNTIME_SYSCALL_CENSUS.md)) showed that the Go linker accepts `-T 0x7e0000000000` and the resulting static binary runs, and that the runtime first tries the heap hint `0xc000000000`. Slice **W6-S01 (spike)** repeats this with the pinned Go version and confirms the runtime tolerates the nucleus returning its own addresses for hint-less `PROT_NONE` reservations.

### Fallback if the spike fails

Keep the Go kernel at the default `0x400000` and build all user programs as **static-PIE** (musl supports it), loading them at a randomised base inside the user ranges. Document the outcome in ADR-0004.

## 3. Page-table ownership

The **nucleus** owns the page-table *mechanism* (walk, map, unmap, TLB flush) and the physmap. The **Go kernel** owns *policy* for user address spaces (which ranges exist, copy-on-write, mmap of files) and asks the nucleus to apply it through `nu_*` calls ([NUCLEUS_ABI.md](NUCLEUS_ABI.md)). The Go kernel reads and writes user memory through the physmap after translating addresses, never by switching CR3.
