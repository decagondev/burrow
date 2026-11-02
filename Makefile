# =============================================================================
# Burrow - top-level Makefile
#
#   make            build disk image (build/disk.img)
#   make run        boot it in QEMU (serial on your terminal, Ctrl-A X quits)
#   make debug      boot paused, waiting for gdb on :1234 (see docs/06-guides)
#   make test       unit tests (host) + smoke test (QEMU)
#   make test-unit  host-only tests: C (gcc) + Go (go test)
#   make smoke      boot headless, assert serial markers, check exit code
#   make regress    boot deliberately broken images, assert they fail loudly
#   make sizes      show how full stage1/stage2/loader are
#   make docs-check parse every Mermaid diagram (needs node + npm)
#   make clean
# =============================================================================

BUILD    := build
NASM     ?= nasm
# On x86-64 Linux the system gcc works. Elsewhere use a cross compiler:
#   make CC_TARGET=x86_64-elf-gcc LD_TARGET=x86_64-elf-ld
CC_TARGET ?= gcc
LD_TARGET ?= ld
OBJCOPY   ?= objcopy
HOSTCC    ?= gcc
GO        ?= go
QEMU      ?= qemu-system-x86_64

# Flags for code that runs on the bare machine.
#  -ffreestanding        no hosted libc assumptions
#  -mno-red-zone         interrupts would clobber the 128-byte red zone
#  -mgeneral-regs-only   SSE is NOT enabled yet (CR4.OSFXSR=0) -> no xmm use
#  -fno-pic -static      we are linked at a fixed address
LOADER_CFLAGS := -std=c11 -O2 -g -Wall -Wextra -Werror \
	-ffreestanding -fno-stack-protector -fno-pic -fno-pie \
	-mno-red-zone -mgeneral-regs-only -mcmodel=small \
	-fno-asynchronous-unwind-tables -fno-builtin \
	-Iinclude -Iloader
LOADER_LDFLAGS := -nostdlib -static -no-pie -T loader/linker.ld -z max-page-size=0x1000

LOADER_C   := $(wildcard loader/*.c)
LOADER_OBJ := $(patsubst loader/%.c,$(BUILD)/loader/%.o,$(LOADER_C)) $(BUILD)/loader/entry.o

# The nucleus runs in the top 2 GiB (-mcmodel=kernel). It reuses the loader's
# freestanding serial/kprintf/string modules, compiled a second time.
NUCLEUS_CFLAGS := $(filter-out -mcmodel=small -Iloader,$(LOADER_CFLAGS)) \
	-mcmodel=kernel -Inucleus -Iloader
NUCLEUS_LDFLAGS := -nostdlib -static -no-pie -T nucleus/linker.ld -z max-page-size=0x1000
NUCLEUS_SHARED := serial kprintf lib_string
NUCLEUS_OBJ := $(patsubst nucleus/%.c,$(BUILD)/nucleus/%.o,$(wildcard nucleus/*.c)) \
	$(patsubst %,$(BUILD)/nucleus/shared_%.o,$(NUCLEUS_SHARED)) $(BUILD)/nucleus/entry.o

QEMU_COMMON := -machine pc -cpu qemu64 -m 128M \
	-drive file=$(BUILD)/disk.img,format=raw,if=ide,index=0,media=disk \
	-no-reboot

.PHONY: all run debug test test-unit test-c test-go smoke regress sizes docs-check clean
all: $(BUILD)/disk.img

# ---- boot sectors -----------------------------------------------------------
$(BUILD)/stage1.bin: boot/stage1.asm boot/boot_abi.inc boot/lib16.inc | $(BUILD)
	$(NASM) -f bin -I boot/ $< -o $@ -l $(BUILD)/stage1.lst

$(BUILD)/stage2.bin: boot/stage2.asm boot/boot_abi.inc boot/lib16.inc | $(BUILD)
	$(NASM) -f bin -I boot/ $< -o $@ -l $(BUILD)/stage2.lst

# ---- loader (C) -------------------------------------------------------------
$(BUILD)/loader/%.o: loader/%.c $(wildcard loader/*.h include/burrow/*.h) | $(BUILD)/loader
	$(CC_TARGET) $(LOADER_CFLAGS) -c $< -o $@

$(BUILD)/loader/entry.o: loader/entry.asm | $(BUILD)/loader
	$(NASM) -f elf64 $< -o $@

$(BUILD)/loader.elf: $(LOADER_OBJ) loader/linker.ld
	$(LD_TARGET) $(LOADER_LDFLAGS) $(LOADER_OBJ) -o $@

$(BUILD)/loader.bin: $(BUILD)/loader.elf
	$(OBJCOPY) -O binary $< $@

# ---- nucleus (C, higher half) -----------------------------------------------
$(BUILD)/nucleus/%.o: nucleus/%.c $(wildcard nucleus/*.h loader/*.h include/burrow/*.h) | $(BUILD)/nucleus
	$(CC_TARGET) $(NUCLEUS_CFLAGS) -c $< -o $@

$(BUILD)/nucleus/shared_%.o: loader/%.c $(wildcard loader/*.h include/burrow/*.h) | $(BUILD)/nucleus
	$(CC_TARGET) $(NUCLEUS_CFLAGS) -c $< -o $@

$(BUILD)/nucleus/entry.o: nucleus/entry.asm | $(BUILD)/nucleus
	$(NASM) -f elf64 $< -o $@

$(BUILD)/nucleus.elf: $(NUCLEUS_OBJ) nucleus/linker.ld
	$(LD_TARGET) $(NUCLEUS_LDFLAGS) $(NUCLEUS_OBJ) -o $@

# The disk copy drops debug info (gdb uses nucleus.elf); the loader reads all of it.
$(BUILD)/nucleus.disk.elf: $(BUILD)/nucleus.elf
	$(OBJCOPY) --strip-debug $< $@

# ---- host tools -------------------------------------------------------------
$(BUILD)/mkimage: $(wildcard tools/mkimage/*.go) | $(BUILD)
	cd tools && $(GO) build -o ../$@ ./mkimage

# ---- disk image -------------------------------------------------------------
$(BUILD)/disk.img: $(BUILD)/stage1.bin $(BUILD)/stage2.bin $(BUILD)/loader.bin \
		$(BUILD)/nucleus.disk.elf $(BUILD)/mkimage
	$(BUILD)/mkimage -o $@ -stage1 $(BUILD)/stage1.bin -stage2 $(BUILD)/stage2.bin \
		-payload loader=$(BUILD)/loader.bin -payload nucleus=$(BUILD)/nucleus.disk.elf

# ---- run / debug ------------------------------------------------------------
run: $(BUILD)/disk.img
	$(QEMU) $(QEMU_COMMON) -serial mon:stdio -display none

debug: $(BUILD)/disk.img
	@echo "QEMU paused. In another terminal: gdb -x scripts/gdbinit"
	$(QEMU) $(QEMU_COMMON) -serial mon:stdio -display none -s -S

# ---- tests ------------------------------------------------------------------
test: test-unit smoke regress

test-unit: test-c test-go

UNIT_SRC := tests/unit/c/test_main.c $(wildcard tests/unit/c/test_*.c) \
	loader/kprintf.c loader/crc32.c loader/manifest.c loader/memmap.c \
	loader/elf64.c loader/lib_string.c loader/palloc.c loader/paging.c loader/image.c \
	loader/elf_load.c
UNIT_SRC := $(sort $(UNIT_SRC))

$(BUILD)/unit_tests: $(UNIT_SRC) $(wildcard tests/unit/c/*.h loader/*.h include/burrow/*.h) | $(BUILD)
	$(HOSTCC) -std=c11 -O0 -g -Wall -Wextra -Werror -fno-builtin \
		-fsanitize=address,undefined -Iinclude -Iloader -Itests/unit/c \
		$(UNIT_SRC) -o $@

test-c: $(BUILD)/unit_tests
	$(BUILD)/unit_tests

test-go:
	cd tools && $(GO) vet ./... && $(GO) test ./...
	cd kernel && $(GO) vet ./... && $(GO) test ./...

# Every expect file must pass on the same boot: older ones guard old markers.
SMOKE_EXPECT := tests/smoke/expect/boot_to_loader.txt tests/smoke/expect/boot_to_nucleus.txt

smoke: $(BUILD)/disk.img
	@for e in $(SMOKE_EXPECT); do QEMU="$(QEMU)" tests/smoke/smoke.sh $(BUILD)/disk.img $$e || exit 1; done

regress: $(BUILD)/disk.img
	QEMU="$(QEMU)" tests/regression/run_all.sh $(BUILD)/disk.img

sizes: $(BUILD)/stage1.bin $(BUILD)/stage2.bin $(BUILD)/loader.bin $(BUILD)/nucleus.disk.elf
	@scripts/sizes.sh $(BUILD)

$(BUILD) $(BUILD)/loader $(BUILD)/nucleus:
	mkdir -p $@

docs-check:
	cd tools/doccheck && npm install --silent && node check.mjs $(addprefix ../../,$(shell find . -name '*.md' -not -path './tools/doccheck/node_modules/*' -not -path './build/*'))

clean:
	rm -rf $(BUILD)
