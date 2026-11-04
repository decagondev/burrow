# W18 Foundations for Bigger Programs  (M18)  — status: planned

Goal: the machinery big programs take for granted: `fork` that shares memory until a write instead of copying it, `execve` that reads a program's pages as they are touched, memory processes share (`MAP_SHARED`, POSIX and SysV shared memory, `memfd_create`), and dynamic linking with musl's loader. Demo: Doom in an X window draws through MIT-SHM and keeps up with the fbdev Doom; a 100 MB process forks in milliseconds; a dynamically linked program and a `dlopen`ed library run on Burrow. Stories: E18 ([BACKLOG.md](../BACKLOG.md)).

Where we start (after W17): fork is an eager copy (`proc.Mem.Clone`, W10: every private page copied into a new frame); `execve` reads the whole file before it starts (BUG-0017: Qt's first frame was mostly loading); `mmap` of files is private only and writable `MAP_SHARED` is `-ENODEV` (W14-S05); there are no SysV calls, so TinyX's MIT-SHM extension (compiled in: `Xext/shm.c`) never works and every X image crosses the socket (W16-S05: Doom in X at a third of its fbdev rate); every program is static. What is ready: user memory is Go's policy over a nucleus that only maps frames (`Map`, `Unmap`, `PageAlloc`, W9), user page faults reach Go with the write bit (`FaultIn`, W12-S03), file-backed areas fill on first touch (W14-S05), and the host's musl has a dynamic loader to compare against.

Design (mechanism in the nucleus, policy in Go, as ever):
- **Frames get owners, not one owner.** A frame may be mapped by several address spaces; `proc` counts references per frame and frees one only when the last mapping goes. Fork maps every private writable page read-only in both processes and marks it copy-on-write; a write fault copies the page (or, for the last owner, just makes it writable again). Kernel writes into user memory (`CopyOut`: `read`, signal frames, futex words) break the share the same way. Shared mappings and device memory are never copy-on-write.
- **A shared object is frames with a name.** `/dev/shm` (POSIX `shm_open`), `memfd_create` and SysV segments are one kind of object: a list of frames, read and written through the file layer and mapped `MAP_SHARED` directly, so every process sees the same bytes. `MAP_SHARED|MAP_ANONYMOUS` makes an unnamed one, which fork shares.
- **Lazy `execve`.** ELF segments become file-backed private areas (the `ReserveFile` of W14-S05) instead of being read in whole; the bss is a zero area. A program pays for the pages it touches.
- **Dynamic linking is user space.** The kernel's part is `PT_INTERP`: load the interpreter as well, start there, and pass `AT_BASE`, `AT_PHDR`, `AT_PHNUM` and `AT_ENTRY`; musl's `ld-musl-x86_64.so.1` (which is its `libc.so`) does the rest with `mmap`, `mprotect` and `open`. The initrd gains `/lib`.

### [ ] W18-S01 Copy-on-write fork (E18)
Reference-counted frames in `proc.Mem`, fork sharing private pages read-only, write faults and `CopyOut` that copy on demand, exit that frees only unshared frames. Host tests with `proctest.Fake` (two processes writing the same page after fork see their own bytes; a page shared three ways; exit order; fork of a fork; a futex word and a `read` buffer in a shared page). A `forkbench` program (fork and exit of a process with 64 MiB touched, and the memory the system lost) on Linux and on Burrow, before and after; `make test` unchanged, since every smoke test forks.

### [ ] W18-S02 Lazy execve (E18)
`LoadELF` maps segments as file-backed private areas and the bss as a zero area, reading nothing up front; the pages fill at first touch. Measured on `make qt-bench` (first frame) and `make x11-bench` (st start), against W15-S05's and W16-S05's numbers, with the budgets tightened where it pays.

### [ ] W18-S03 Shared memory: MAP_SHARED, /dev/shm and memfd (E18)
Shared objects in a `kernel/proc` (or `kernel/fs/shmfs`) package: `/dev/shm` files (`shm_open`, `shm_unlink` are open and unlink there), `memfd_create`, `ftruncate` growing and shrinking them, `read`/`write`/`mmap(MAP_SHARED)` all on the same frames; `MAP_SHARED|MAP_ANONYMOUS` shared across fork; `msync` accepted. Writable `MAP_SHARED` of files elsewhere stays `-ENODEV`. A `shmtest` program (two processes through each kind) on Linux and on Burrow.

### [ ] W18-S04 SysV shared memory and MIT-SHM (E18)
`shmget`, `shmat`, `shmdt`, `shmctl` (`IPC_STAT`, `IPC_RMID`: gone at the last detach) over S03's objects. TinyX's MIT-SHM then works as built; Doom's X backend uses `XShmPutImage` when the server offers it (falling back to `XPutImage`), and `make x11-bench`'s Doom share rises (the budget with it). `x11-census` and `x11-smoke` keep passing; xhello learns to report whether MIT-SHM is there.

### [ ] W18-S05 Dynamic linking (E18)
`PT_INTERP` in `execve` with the auxiliary vector musl's loader reads; `/lib/ld-musl-x86_64.so.1` (musl's `libc.so`, from the ports' toolchain) on the initrd; a dynamic `dynhello`, a shared library it links against, and a `dlopen`/`dlsym` test, checked on the host's Linux first and then on Burrow in `make ports-smoke`. TinyCC's `-run` (which needs a dynamic `tcc`) if it comes for free.

### [ ] W18-S06 Budgets, the memory guide, M18 (E18)
`make mem-bench` (not in `make test`): fork of a big process, `execve` of Qt's largest program, Doom in X with MIT-SHM, memory after a desktop session, against budgets. `docs/02-architecture/MEMORY_MAP.md` gains user memory's new rules (shared frames, copy-on-write, shared objects); PORTING.md gains dynamic linking; DESKTOP.md's "No MIT-SHM" changes.

Exit: **M18 demo — bigger programs.** Doom in X with MIT-SHM at no less than three quarters of the fbdev frame rate; fork of a 64 MiB process under budget; `dynhello` and `dlopen` on Burrow; `make clean test` green; DEVLOG wave entry.

Risks and pitfalls (PIT-NN entries go into BUGS_AND_MITIGATIONS.md when the wave starts):
- **Copy-on-write is easy to get almost right.** Every path that writes user memory from the kernel must break the share (`CopyOut`, the signal frame, `futex` wake-ups that write, `clear_child_tid`, the ELF loader); a missed one corrupts another process silently. Host tests write through each path after a fork.
- **Stale translations.** Making a page read-only in a running process needs its TLB entry gone; the nucleus maps and unmaps with `invlpg` on the current space only, so remapping in a space that is not current must not leave an old writable entry behind when it runs again (one CPU: a CR3 load flushes, but check the global-page rules).
- **Frames freed twice or never.** Reference counts across fork, exit, `munmap`, `mremap`, `execve` and shared objects; a census of free frames before and after the smoke tests (the `mem` command) catches leaks.
- **Shared objects outlive processes.** SysV segments stay until `IPC_RMID` and the last detach; `/dev/shm` files until unlinked; a memfd until its last descriptor and mapping go.
- **Dynamic programs read more of the system.** `ld.so` looks for `/etc/ld-musl-x86_64.path` and the library path, and wants `mmap` with `MAP_FIXED` over its own reservations and `mprotect` on file mappings; the census of a dynamic program on Linux shows what else.
