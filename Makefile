# =============================================================================
# Burrow - top-level Makefile
#
#   make            build disk image (build/disk.img)
#   make run        boot it in QEMU (serial on your terminal, Ctrl-A X quits)
#   make debug      boot paused, waiting for gdb on :1234 (see docs/06-guides)
#   make test       unit tests (host) + smoke + go-smoke + ktest + regression (QEMU)
#   make go-smoke   boot build/go.img (nucleus + Go kernel), check the Go markers
#   make run-go     boot the Go image interactively
#   make test-unit  host-only tests: C (gcc) + Go (go test)
#   make smoke      boot headless, assert serial markers, check exit code
#   make ktest      boot one image per tests/ktest suite, run in-kernel tests
#   make regress    boot deliberately broken images, assert they fail loudly
#   make census     Go runtime syscalls on the host (strace) vs the pinned set
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
	-Iinclude -Icommon -Iloader
LOADER_LDFLAGS := -nostdlib -static -no-pie -T loader/linker.ld -z max-page-size=0x1000

# common/ holds freestanding code shared by the loader and the nucleus. Each
# side compiles it with its own flags (the nucleus needs -mcmodel=kernel).
COMMON_C   := $(wildcard common/*.c)
COMMON_H   := $(wildcard common/*.h include/burrow/*.h)

LOADER_C   := $(wildcard loader/*.c)
LOADER_OBJ := $(patsubst loader/%.c,$(BUILD)/loader/%.o,$(LOADER_C)) \
	$(patsubst common/%.c,$(BUILD)/loader/common_%.o,$(COMMON_C)) $(BUILD)/loader/entry.o

# The nucleus runs in the top 2 GiB (-mcmodel=kernel).
NUCLEUS_CFLAGS := $(filter-out -mcmodel=small -Iloader,$(LOADER_CFLAGS)) \
	-mcmodel=kernel -Inucleus
NUCLEUS_LDFLAGS := -nostdlib -static -no-pie -T nucleus/linker.ld -z max-page-size=0x1000
NUCLEUS_ASM := $(wildcard nucleus/*.asm)
NUCLEUS_OBJ := $(patsubst nucleus/%.c,$(BUILD)/nucleus/%.o,$(wildcard nucleus/*.c)) \
	$(patsubst common/%.c,$(BUILD)/nucleus/common_%.o,$(COMMON_C)) \
	$(patsubst nucleus/%.asm,$(BUILD)/nucleus/%_asm.o,$(NUCLEUS_ASM))

QEMU_COMMON := -machine pc -cpu qemu64 -m 128M \
	-drive file=$(BUILD)/disk.img,format=raw,if=ide,index=0,media=disk \
	-no-reboot

.PHONY: all run run-go hostsim debug test test-unit test-c test-go census smoke go-smoke ktest regress sizes docs-check clean
all: $(BUILD)/disk.img

# ---- boot sectors -----------------------------------------------------------
$(BUILD)/stage1.bin: boot/stage1.asm boot/boot_abi.inc boot/lib16.inc | $(BUILD)
	$(NASM) -f bin -I boot/ $< -o $@ -l $(BUILD)/stage1.lst

$(BUILD)/stage2.bin: boot/stage2.asm boot/boot_abi.inc boot/lib16.inc | $(BUILD)
	$(NASM) -f bin -I boot/ $< -o $@ -l $(BUILD)/stage2.lst

# ---- loader (C) -------------------------------------------------------------
$(BUILD)/loader/%.o: loader/%.c $(wildcard loader/*.h) $(COMMON_H) | $(BUILD)/loader
	$(CC_TARGET) $(LOADER_CFLAGS) -c $< -o $@

$(BUILD)/loader/common_%.o: common/%.c $(COMMON_H) | $(BUILD)/loader
	$(CC_TARGET) $(LOADER_CFLAGS) -c $< -o $@

$(BUILD)/loader/entry.o: loader/entry.asm | $(BUILD)/loader
	$(NASM) -f elf64 $< -o $@

$(BUILD)/loader.elf: $(LOADER_OBJ) loader/linker.ld
	$(LD_TARGET) $(LOADER_LDFLAGS) $(LOADER_OBJ) -o $@

$(BUILD)/loader.bin: $(BUILD)/loader.elf
	$(OBJCOPY) -O binary $< $@

# ---- nucleus (C, higher half) -----------------------------------------------
$(BUILD)/nucleus/%.o: nucleus/%.c $(wildcard nucleus/*.h) $(COMMON_H) | $(BUILD)/nucleus
	$(CC_TARGET) $(NUCLEUS_CFLAGS) -c $< -o $@

$(BUILD)/nucleus/common_%.o: common/%.c $(COMMON_H) | $(BUILD)/nucleus
	$(CC_TARGET) $(NUCLEUS_CFLAGS) -c $< -o $@

$(BUILD)/nucleus/%_asm.o: nucleus/%.asm $(wildcard nucleus/*.inc) | $(BUILD)/nucleus
	$(NASM) -f elf64 -I nucleus/ $< -o $@

$(BUILD)/nucleus.elf: $(NUCLEUS_OBJ) nucleus/linker.ld
	$(LD_TARGET) $(NUCLEUS_LDFLAGS) $(NUCLEUS_OBJ) -o $@

# The disk copy drops debug info (gdb uses nucleus.elf); the loader reads all of it.
$(BUILD)/nucleus.disk.elf: $(BUILD)/nucleus.elf
	$(OBJCOPY) --strip-debug $< $@

# ---- Go kernel (stock GOOS=linux binary, GO_RUNTIME_HOSTING.md §2) ---------
# -T puts its text high in the lower half (MEMORY_MAP.md §2); the nucleus maps
# its segments itself, the loader only hands the file over as a boot module.
GO_KERNEL_TEXT := 0x7e0000000000
GO_KERNEL_SRC  := $(shell find kernel -name '*.go' -o -name '*.s') kernel/go.mod

$(BUILD)/kernel.elf: $(GO_KERNEL_SRC) | $(BUILD)
	cd kernel && CGO_ENABLED=0 GOOS=linux GOARCH=amd64 $(GO) build -tags burrow -trimpath \
		-ldflags='-T $(GO_KERNEL_TEXT) -s' -o ../$@ ./cmd/kernel

# ---- host tools -------------------------------------------------------------
$(BUILD)/mkimage: $(wildcard tools/mkimage/*.go) | $(BUILD)
	cd tools && $(GO) build -o ../$@ ./mkimage

# ---- disk image -------------------------------------------------------------
BOOT_PARTS := $(BUILD)/stage1.bin $(BUILD)/stage2.bin $(BUILD)/loader.bin \
	$(BUILD)/nucleus.disk.elf $(BUILD)/mkimage
MKIMAGE_BOOT = $(BUILD)/mkimage -o $@ -stage1 $(BUILD)/stage1.bin -stage2 $(BUILD)/stage2.bin \
	-payload loader=$(BUILD)/loader.bin -payload nucleus=$(BUILD)/nucleus.disk.elf

$(BUILD)/disk.img: $(BOOT_PARTS)
	$(MKIMAGE_BOOT)

# The initial file tree (W8): initrd/ packed as ustar, reproducibly (sorted
# names, owner 0, mtime 0, modes 644/755 whatever the checkout says),
# mounted at / by the Go kernel (kernel/fs/tarfs).
INITRD_FILES := $(shell find initrd -type f)
$(BUILD)/initrd.tar: $(INITRD_FILES) | $(BUILD)
	tar --format=ustar --sort=name --owner=0 --group=0 --numeric-owner --mtime=@0 \
		--mode=a-rwx,u+rwX,go+rX -C initrd -cf $@ .

# The same boot chain plus the Go kernel and the initrd. Separate from
# disk.img so the nucleus images stay fast (1.7 MiB over ATA PIO costs
# ~1.5 s per boot).
$(BUILD)/go.img: $(BOOT_PARTS) $(BUILD)/kernel.elf $(BUILD)/initrd.tar
	$(MKIMAGE_BOOT) -payload kernel=$(BUILD)/kernel.elf -payload initrd=$(BUILD)/initrd.tar >/dev/null

# One image per ktest suite: the same boot chain plus a "ktest" payload that
# names the in-kernel tests to run (nucleus/ktest.h).
KTEST_SUITES := $(basename $(notdir $(wildcard tests/ktest/*.txt)))

$(BUILD)/ktest-%.img: tests/ktest/%.txt $(BOOT_PARTS)
	$(MKIMAGE_BOOT) -payload ktest=$< >/dev/null

# ---- run / debug ------------------------------------------------------------
run: $(BUILD)/disk.img
	$(QEMU) $(QEMU_COMMON) -serial mon:stdio -display none

debug: $(BUILD)/disk.img
	@echo "QEMU paused. In another terminal: gdb -x scripts/gdbinit"
	$(QEMU) $(QEMU_COMMON) -serial mon:stdio -display none -s -S

# ---- tests ------------------------------------------------------------------
test: test-unit census smoke go-smoke ktest regress

test-unit: test-c test-go

# Host-testable sources: everything in common/ except serial.c (port I/O),
# plus the pure modules of the loader and the nucleus.
UNIT_SRC := tests/unit/c/test_main.c $(wildcard tests/unit/c/test_*.c) \
	$(filter-out common/serial.c,$(COMMON_C)) \
	loader/manifest.c loader/memmap.c loader/palloc.c \
	loader/image.c loader/elf_load.c loader/vmsetup.c \
	nucleus/pmm.c nucleus/regions.c nucleus/go_stack.c
UNIT_SRC := $(sort $(UNIT_SRC))

$(BUILD)/unit_tests: $(UNIT_SRC) $(wildcard tests/unit/c/*.h loader/*.h nucleus/*.h) $(COMMON_H) | $(BUILD)
	$(HOSTCC) -std=c11 -O0 -g -Wall -Wextra -Werror -fno-builtin \
		-fsanitize=address,undefined -Iinclude -Icommon -Iloader -Inucleus -Itests/unit/c \
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

# Boot the Go kernel, type into its console (tests/smoke/keys/) and check its
# markers (tests/smoke/expect/boot_to_go.txt).
go-smoke: $(BUILD)/go.img
	QEMU="$(QEMU)" tests/smoke/smoke.sh $(BUILD)/go.img tests/smoke/expect/boot_to_go.txt 30 tests/smoke/keys/boot_to_go.keys

# The Go console and drivers on simulated hardware, as a host program (kernel/hostsim).
hostsim:
	cd kernel && $(GO) run ./cmd/hostsim

# Boot the Go image interactively: the console reads the serial line (Ctrl-A X quits).
run-go: $(BUILD)/go.img
	$(QEMU) $(subst $(BUILD)/disk.img,$(BUILD)/go.img,$(QEMU_COMMON)) -serial mon:stdio -display none

ktest: $(patsubst %,$(BUILD)/ktest-%.img,$(KTEST_SUITES))
	@for s in $(KTEST_SUITES); do QEMU="$(QEMU)" tests/smoke/smoke.sh $(BUILD)/ktest-$$s.img tests/ktest/expect/$$s.txt || exit 1; done

regress: $(BUILD)/disk.img $(BUILD)/go.img
	QEMU="$(QEMU)" tests/regression/run_all.sh $(BUILD)/disk.img

# PIT-09: the Go runtime's syscall set (host strace) vs the pinned census.
census:
	GO="$(GO)" tests/regression/census.sh $(BUILD)

sizes: $(BUILD)/stage1.bin $(BUILD)/stage2.bin $(BUILD)/loader.bin $(BUILD)/nucleus.disk.elf
	@scripts/sizes.sh $(BUILD)

$(BUILD) $(BUILD)/loader $(BUILD)/nucleus:
	mkdir -p $@

docs-check:
	cd tools/doccheck && npm install --silent && node check.mjs $(addprefix ../../,$(shell find . -name '*.md' -not -path './tools/doccheck/node_modules/*' -not -path './build/*'))

clean:
	rm -rf $(BUILD)
