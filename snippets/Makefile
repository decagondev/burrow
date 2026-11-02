# make -C snippets check  -> proves the reference snippets still compile.
CFLAGS_K := -std=c11 -Wall -Wextra -Werror -ffreestanding -mno-red-zone -mcmodel=kernel -fno-pic
check:
	nasm -f elf64 nucleus/syscall_gate.asm -o /tmp/snip_gate.o
	gcc $(CFLAGS_K) -c nucleus/syscall_shim.c -o /tmp/snip_shim.o
	gcc $(CFLAGS_K) -c nucleus/go_initial_stack.c -o /tmp/snip_stack.o
	gofmt -l kernel/ | (! grep .)
	@echo "snippets OK (doom backend needs doomgeneric headers: see userland/)"
