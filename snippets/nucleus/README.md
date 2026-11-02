# Nucleus reference snippets (Waves 5-7)

NOT part of the build yet. These are reviewed, compile-checked starting
points for the slices that create `nucleus/`. Copy a file into `nucleus/`
when you reach its slice, then make it pass that slice's tests.

| File | Slice | What it teaches |
|---|---|---|
| `nucleus_api.h` | W5 | The nucleus' internal interfaces (threads, VM, console) |
| `syscall_gate.asm` | W6-S03 | SYSCALL entry: stack switch, caller classification, iretq/sysret return |
| `syscall_shim.c` | W6-S04.. | Table-driven Linux-syscall subset for the Go runtime |
| `go_initial_stack.c` | W6-S02 | Building argc/argv/envp/auxv exactly like Linux execve |
| `linker.ld` | W5-S01 | Higher-half link for `-mcmodel=kernel` |

Check they still compile: `make -C snippets check` (see Makefile here).
