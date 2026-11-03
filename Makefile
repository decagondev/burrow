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

# The nucleus-only images run in 128 MiB; the Go images get more (W14-S01).
GO_MEM ?= 512M
QEMU_COMMON := -machine pc -cpu qemu64 -m 128M \
	-drive file=$(BUILD)/disk.img,format=raw,if=ide,index=0,media=disk \
	-no-reboot

.PHONY: all run run-go run-busybox run-doom hostsim debug test test-unit test-c test-go census fat-interop smoke go-smoke devices-smoke disk-smoke run-dev dev-smoke dev-smoke-run ports-smoke mem-smoke ports ports-dl-smoke ports-dl-smoke-run doom doom-smoke doom-smoke-run doom-bench busybox busybox-smoke busybox-smoke-run ktest regress sizes docs-check site clean
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

# User programs (W9): static Linux x86-64 executables from user/, no libc,
# installed in the initrd's /bin.
USER_ASM  := $(wildcard user/*.asm)
USER_C    := $(wildcard user/*.c)
USER_BINS := $(patsubst user/%.asm,$(BUILD)/user/%,$(USER_ASM)) $(patsubst user/%.c,$(BUILD)/user/%,$(USER_C))
# Go user programs (W12-S03): user/go/<name>/ built as static GOOS=linux
# binaries by the host's Go toolchain (no cgo), like any Linux Go program.
USER_GO   := $(patsubst user/go/%/main.go,$(BUILD)/user/%,$(wildcard user/go/*/main.go))
USER_BINS += $(USER_GO)
$(BUILD)/user/%: user/go/%/main.go user/go/go.mod | $(BUILD)/user
	cd user/go && GOOS=linux GOARCH=amd64 CGO_ENABLED=0 $(GO) build -trimpath -ldflags=-s -o $(abspath $@) ./$*
$(BUILD)/user/%.o: user/%.asm | $(BUILD)/user
	$(NASM) -f elf64 $< -o $@
$(BUILD)/user/%: $(BUILD)/user/%.o
	$(LD_TARGET) -static -nostdlib -o $@ $<
# C programs: static musl (W9-S06). Debian/Ubuntu: sudo apt install musl-tools.
MUSL_GCC ?= musl-gcc
$(BUILD)/user/%: user/%.c $(BUILD)/uapi/.stamp | $(BUILD)/user
	@command -v $(MUSL_GCC) >/dev/null || { echo "$(MUSL_GCC) not found: install musl-tools (sudo apt install musl-tools) or set MUSL_GCC=..."; exit 1; }
	$(MUSL_GCC) -static -Os -isystem $(BUILD)/uapi -o $@ $<
# Linux UAPI headers (linux/, asm/, asm-generic/) for programs that use the
# device interfaces (fbdev, evdev, W11): links to the host's linux-libc-dev
# (links, not copies: some names differ only in case, which a Windows-backed
# checkout cannot hold), never glibc's own headers, so musl sees only kernel ones.
UAPI_INCLUDE ?= /usr/include
UAPI_ASM     ?= /usr/include/x86_64-linux-gnu/asm
$(BUILD)/uapi/.stamp: | $(BUILD)
	@test -f $(UAPI_INCLUDE)/linux/fb.h || { echo "no Linux UAPI headers in $(UAPI_INCLUDE): sudo apt install linux-libc-dev"; exit 1; }
	rm -rf $(BUILD)/uapi && mkdir -p $(BUILD)/uapi
	ln -s $(UAPI_INCLUDE)/linux $(UAPI_INCLUDE)/asm-generic $(BUILD)/uapi/
	ln -s $(UAPI_ASM) $(BUILD)/uapi/asm
	touch $@

# The initial file tree (W8): initrd/ plus the user programs in bin/, packed
# as ustar reproducibly (sorted names, owner 0, mtime 0, modes 644/755
# whatever the checkout says), mounted at / by the Go kernel (kernel/fs/tarfs).
INITRD_FILES := $(shell find initrd -type f)
$(BUILD)/initrd.tar: $(INITRD_FILES) $(USER_BINS) | $(BUILD)
	rm -rf $(BUILD)/initrd-root && mkdir -p $(BUILD)/initrd-root/bin
	cp -R initrd/. $(BUILD)/initrd-root/
	cp $(USER_BINS) $(BUILD)/initrd-root/bin/
	tar --format=ustar --sort=name --owner=0 --group=0 --numeric-owner --mtime=@0 \
		--mode=a-rwx,u+rwX,go+rX -C $(BUILD)/initrd-root -cf $@ .

# busybox (W10-S08): `make busybox` downloads the pinned source release once
# (scripts/busybox.sh, sha256-checked) and builds build/busybox. It goes only
# on build/go-busybox.img, whose initrd is the usual one plus /bin/busybox, so
# go.img and its expect file stay as they are.
busybox: | $(BUILD)
	MUSL_GCC="$(MUSL_GCC)" scripts/busybox.sh $(BUILD)/busybox
$(BUILD)/initrd-busybox.tar: $(BUILD)/initrd.tar $(BUILD)/busybox
	rm -rf $(BUILD)/initrd-busybox-root && cp -R $(BUILD)/initrd-root $(BUILD)/initrd-busybox-root
	cp $(BUILD)/busybox $(BUILD)/initrd-busybox-root/bin/busybox
	tar --format=ustar --sort=name --owner=0 --group=0 --numeric-owner --mtime=@0 \
		--mode=a-rwx,u+rwX,go+rX -C $(BUILD)/initrd-busybox-root -cf $@ .
$(BUILD)/go-busybox.img: $(BOOT_PARTS) $(BUILD)/kernel.elf $(BUILD)/initrd-busybox.tar
	$(MKIMAGE_BOOT) -payload kernel=$(BUILD)/kernel.elf -payload initrd=$(BUILD)/initrd-busybox.tar >/dev/null

# Doom (W11): `make doom` downloads doomgeneric (pinned commit) and Freedoom
# (pinned sha256) once (scripts/doom.sh) and builds build/doom-root/doom with
# the Burrow backend, plus freedoom1.wad. The game boots from go-doom.img
# (the usual initrd plus /bin/doom) with the WAD on doom-data.img, a FAT32
# disk mounted at /mnt; go.img and data.img stay as they are.
doom: $(BUILD)/uapi/.stamp
	MUSL_GCC="$(MUSL_GCC)" scripts/doom.sh $(BUILD)/doom-root $(abspath $(BUILD)/uapi)
$(BUILD)/initrd-doom.tar: $(BUILD)/initrd.tar $(BUILD)/doom-root/doom
	rm -rf $(BUILD)/initrd-doom-root && cp -R $(BUILD)/initrd-root $(BUILD)/initrd-doom-root
	cp $(BUILD)/doom-root/doom $(BUILD)/initrd-doom-root/bin/doom
	tar --format=ustar --sort=name --owner=0 --group=0 --numeric-owner --mtime=@0 \
		--mode=a-rwx,u+rwX,go+rX -C $(BUILD)/initrd-doom-root -cf $@ .
$(BUILD)/go-doom.img: $(BOOT_PARTS) $(BUILD)/kernel.elf $(BUILD)/initrd-doom.tar
	$(MKIMAGE_BOOT) -payload kernel=$(BUILD)/kernel.elf -payload initrd=$(BUILD)/initrd-doom.tar >/dev/null
DOOM_DRIVE := -drive file=$(BUILD)/doom-data.img,format=raw,if=ide,index=1,media=disk
$(BUILD)/doom-data.img: $(BUILD)/doom-root/freedoom1.wad $(wildcard kernel/fs/fat/*.go kernel/cmd/mkfat/*.go)
	rm -rf $(BUILD)/doom-data && mkdir -p $(BUILD)/doom-data
	cp $(BUILD)/doom-root/freedoom1.wad $(BUILD)/doom-data/
	cd kernel && $(GO) run ./cmd/mkfat -o ../$@ -label doomdata -size 48 ../$(BUILD)/doom-data

# The data disk (W8): datadisk/ as FAT32, the primary slave, mounted at /mnt.
DATADISK_FILES := $(shell find datadisk -type f)
DATA_DRIVE := -drive file=$(BUILD)/data.img,format=raw,if=ide,index=1,media=disk
$(BUILD)/data.img: $(DATADISK_FILES) $(wildcard kernel/fs/fat/*.go kernel/cmd/mkfat/*.go) | $(BUILD)
	cd kernel && $(GO) run ./cmd/mkfat -o ../$@ -label burrowdata ../datadisk

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
test: test-unit census fat-interop smoke go-smoke devices-smoke disk-smoke ports-smoke mem-smoke ports-dl-smoke dev-smoke doom-smoke busybox-smoke ktest regress

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
	cd user/go && $(GO) vet ./...

# Every expect file must pass on the same boot: older ones guard old markers.
SMOKE_EXPECT := tests/smoke/expect/boot_to_loader.txt tests/smoke/expect/boot_to_nucleus.txt

smoke: $(BUILD)/disk.img
	@for e in $(SMOKE_EXPECT); do QEMU="$(QEMU)" tests/smoke/smoke.sh $(BUILD)/disk.img $$e || exit 1; done

# Boot the Go kernel, type into its console (tests/smoke/keys/) and check its
# markers (tests/smoke/expect/boot_to_go.txt).
go-smoke: $(BUILD)/go.img $(BUILD)/data.img
	MEM=$(GO_MEM) QEMU="$(QEMU)" QEMU_EXTRA="$(DATA_DRIVE)" tests/smoke/smoke.sh $(BUILD)/go.img tests/smoke/expect/boot_to_go.txt 60 tests/smoke/keys/boot_to_go.keys

# The devices programs use (W11): /dev/fb0 drawn and its pixels read back
# (tests/smoke/expect/devices.screen), /dev/input/event0.
devices-smoke: $(BUILD)/go.img $(BUILD)/data.img
	MEM=$(GO_MEM) QEMU="$(QEMU)" QEMU_EXTRA="$(DATA_DRIVE)" tests/smoke/smoke.sh $(BUILD)/go.img tests/smoke/expect/devices.txt 60 tests/smoke/keys/devices.keys

# Doom typed into (W11-S05); skips until `make doom` ran.
doom-smoke:
	@if [ -f $(BUILD)/doom-root/doom ]; then $(MAKE) --no-print-directory doom-smoke-run; \
	else echo "DOOM SMOKE SKIP (make doom downloads and builds it)"; fi
doom-smoke-run: $(BUILD)/go-doom.img $(BUILD)/doom-data.img
	MEM=$(GO_MEM) QEMU="$(QEMU)" QEMU_EXTRA="$(DOOM_DRIVE)" tests/smoke/smoke.sh $(BUILD)/go-doom.img tests/smoke/expect/doom.txt 120 tests/smoke/keys/doom.keys

# Doom's own benchmark (W11-S06, RISKS R-07): -timedemo demo1 as fast as it
# goes (about 70 s in QEMU, so not part of make test); fails below 20 fps.
doom-bench: $(BUILD)/go-doom.img $(BUILD)/doom-data.img
	MEM=$(GO_MEM) QEMU="$(QEMU)" QEMU_EXTRA="$(DOOM_DRIVE)" tests/smoke/smoke.sh $(BUILD)/go-doom.img tests/smoke/expect/doom-bench.txt 300 tests/smoke/keys/doom-bench.keys
	@python3 -c 'import re,sys; m=re.search(r"\(([0-9.]+) fps\)", open(sys.argv[1], errors="replace").read()); fps=float(m.group(1)) if m else 0; print("DOOM BENCH %.1f fps (timedemo demo1)" % fps); sys.exit(fps < 20)' $(BUILD)/smoke-doom-bench.log

# Disk writes (W13-S03): a kernel argument (disktest=hdb) makes the Go kernel
# overwrite the end of a fresh blank scratch disk, flush and read it back;
# then tests/smoke/checkdisk.py checks the image file on the host, so the
# bytes are known to have reached the disk.
$(BUILD)/go-disktest.img: $(BOOT_PARTS) $(BUILD)/kernel.elf $(BUILD)/initrd.tar
	echo disktest=hdb > $(BUILD)/kargs-disktest.txt
	$(MKIMAGE_BOOT) -payload kernel=$(BUILD)/kernel.elf -payload initrd=$(BUILD)/initrd.tar \
		-payload kargs=$(BUILD)/kargs-disktest.txt >/dev/null
disk-smoke: $(BUILD)/go-disktest.img
	rm -f $(BUILD)/scratch.img && truncate -s 1M $(BUILD)/scratch.img
	MEM=$(GO_MEM) QEMU="$(QEMU)" QEMU_EXTRA="-drive file=$(BUILD)/scratch.img,format=raw,if=ide,index=1,media=disk" \
		tests/smoke/smoke.sh $(BUILD)/go-disktest.img tests/smoke/expect/disk.txt 60 tests/smoke/keys/disk.keys
	python3 tests/smoke/checkdisk.py $(BUILD)/scratch.img 64

# Ported and multi-threaded programs (W12): threads (musl pthreads), more to come.
# Memory (W14-S01): on a 2 GiB guest, 1.5 GiB touched and checked, then a
# program killed for running out of memory, and the memory back afterwards.
mem-smoke: $(BUILD)/go.img $(BUILD)/data.img
	MEM=2G QEMU="$(QEMU)" QEMU_EXTRA="$(DATA_DRIVE)" tests/smoke/smoke.sh $(BUILD)/go.img tests/smoke/expect/mem.txt 300 tests/smoke/keys/mem.keys

ports-smoke: $(BUILD)/go.img $(BUILD)/data.img $(BUILD)/user/startup
	$(BUILD)/user/startup > $(BUILD)/startup-linux.log  # W14-S03: Linux answers alike
	MEM=$(GO_MEM) QEMU="$(QEMU)" QEMU_EXTRA="$(DATA_DRIVE)" tests/smoke/smoke.sh $(BUILD)/go.img tests/smoke/expect/ports.txt 60 tests/smoke/keys/ports.keys

# Downloaded ports (W12-S04..S06): `make ports` fetches Lua, TinyCC and a
# musl C++ cross toolchain once (scripts/ports.sh, sha256-pinned) and builds
# build/ports-root, a tree like the target's (/bin/lua, /bin/cxxtest,
# /bin/tcc, a C library in /usr). It ships only on go-ports.img.
ports: | $(BUILD)
	scripts/ports.sh $(BUILD)/ports-root
$(BUILD)/initrd-ports.tar: $(BUILD)/initrd.tar $(BUILD)/ports-root/bin/lua
	rm -rf $(BUILD)/initrd-ports-root && cp -R $(BUILD)/initrd-root $(BUILD)/initrd-ports-root
	cp -R $(BUILD)/ports-root/. $(BUILD)/initrd-ports-root/
	tar --format=ustar --sort=name --owner=0 --group=0 --numeric-owner --mtime=@0 \
		--mode=a-rwx,u+rwX,go+rX -C $(BUILD)/initrd-ports-root -cf $@ .
$(BUILD)/go-ports.img: $(BOOT_PARTS) $(BUILD)/kernel.elf $(BUILD)/initrd-ports.tar
	$(MKIMAGE_BOOT) -payload kernel=$(BUILD)/kernel.elf -payload initrd=$(BUILD)/initrd-ports.tar >/dev/null
ports-dl-smoke:
	@if [ -f $(BUILD)/ports-root/bin/lua ]; then $(MAKE) --no-print-directory ports-dl-smoke-run; \
	else echo "PORTS SMOKE SKIP (make ports downloads and builds Lua, C++ and TinyCC)"; fi
ports-dl-smoke-run: $(BUILD)/go-ports.img $(BUILD)/data.img
	MEM=$(GO_MEM) QEMU="$(QEMU)" QEMU_EXTRA="$(DATA_DRIVE)" tests/smoke/smoke.sh $(BUILD)/go-ports.img tests/smoke/expect/ports-dl.txt 90 tests/smoke/keys/ports-dl.keys

# The workbench (W13-S05): busybox (sh, vi), Lua and TinyCC on one image,
# and a home disk that persists. $(HOME_IMG) (repository root, git-ignored)
# is made once from workbench/ with the label BURROWHOME, which the kernel
# mounts at /home; make never rebuilds it: delete it to start again.
# Needs `make busybox ports` first.
HOME_IMG    ?= burrow-home.img
DEV_DISPLAY ?= gtk
$(HOME_IMG):
	cd kernel && $(GO) run ./cmd/mkfat -o $(abspath $@) -label burrowhome -size 64 ../workbench
$(BUILD)/initrd-dev.tar: $(BUILD)/initrd.tar $(BUILD)/busybox $(BUILD)/ports-root/bin/lua
	rm -rf $(BUILD)/initrd-dev-root && cp -R $(BUILD)/initrd-root $(BUILD)/initrd-dev-root
	cp $(BUILD)/busybox $(BUILD)/initrd-dev-root/bin/busybox
	cp -R $(BUILD)/ports-root/. $(BUILD)/initrd-dev-root/
	tar --format=ustar --sort=name --owner=0 --group=0 --numeric-owner --mtime=@0 \
		--mode=a-rwx,u+rwX,go+rX -C $(BUILD)/initrd-dev-root -cf $@ .
$(BUILD)/go-dev.img: $(BOOT_PARTS) $(BUILD)/kernel.elf $(BUILD)/initrd-dev.tar
	$(MKIMAGE_BOOT) -payload kernel=$(BUILD)/kernel.elf -payload initrd=$(BUILD)/initrd-dev.tar >/dev/null
run-dev: $(BUILD)/go-dev.img $(HOME_IMG)
	$(QEMU) $(subst $(BUILD)/disk.img,$(BUILD)/go-dev.img,$(QEMU_COMMON)) -m $(GO_MEM) \
		-drive file=$(HOME_IMG),format=raw,if=ide,index=1,media=disk -serial mon:stdio -display $(DEV_DISPLAY)

# Two boots on a fresh test home (never $(HOME_IMG)): edit with vi, compile
# with tcc, write and rename a Lua script; power off; boot again and find
# all of it; then fsck.fat checks the disk. Skips without busybox and ports.
dev-smoke:
	@if [ -f $(BUILD)/busybox ] && [ -f $(BUILD)/ports-root/bin/lua ]; then $(MAKE) --no-print-directory dev-smoke-run; \
	else echo "DEV SMOKE SKIP (make busybox ports builds what the workbench needs)"; fi
dev-smoke-run: $(BUILD)/go-dev.img
	rm -f $(BUILD)/home-test.img
	cd kernel && $(GO) run ./cmd/mkfat -o ../$(BUILD)/home-test.img -label burrowhome -size 64 ../workbench
	MEM=$(GO_MEM) QEMU="$(QEMU)" QEMU_EXTRA="-drive file=$(BUILD)/home-test.img,format=raw,if=ide,index=1,media=disk" \
		tests/smoke/smoke.sh $(BUILD)/go-dev.img tests/smoke/expect/dev-1.txt 120 tests/smoke/keys/dev-1.keys
	MEM=$(GO_MEM) QEMU="$(QEMU)" QEMU_EXTRA="-drive file=$(BUILD)/home-test.img,format=raw,if=ide,index=1,media=disk" \
		tests/smoke/smoke.sh $(BUILD)/go-dev.img tests/smoke/expect/dev-2.txt 90 tests/smoke/keys/dev-2.keys
	@if command -v fsck.fat >/dev/null; then fsck.fat -n $(BUILD)/home-test.img >/dev/null && echo "HOME DISK PASS (fsck.fat after two boots)"; \
	else echo "HOME DISK SKIP (no fsck.fat)"; fi

# busybox sh typed into the console (W10-S08); skips until `make busybox` ran.
busybox-smoke:
	@if [ -f $(BUILD)/busybox ]; then $(MAKE) --no-print-directory busybox-smoke-run; \
	else echo "BUSYBOX SMOKE SKIP (make busybox downloads and builds it)"; fi
busybox-smoke-run: $(BUILD)/go-busybox.img $(BUILD)/data.img
	MEM=$(GO_MEM) QEMU="$(QEMU)" QEMU_EXTRA="$(DATA_DRIVE)" tests/smoke/smoke.sh $(BUILD)/go-busybox.img tests/smoke/expect/busybox.txt 90 tests/smoke/keys/busybox.keys

# The Go console and drivers on simulated hardware, as a host program (kernel/hostsim).
hostsim:
	cd kernel && $(GO) run ./cmd/hostsim

# Boot the Go image interactively: the console reads the serial line (Ctrl-A X
# quits). GO_DISPLAY=gtk (or sdl) also shows the screen and takes the keyboard.
GO_DISPLAY ?= none
run-go: $(BUILD)/go.img $(BUILD)/data.img
	$(QEMU) $(subst $(BUILD)/disk.img,$(BUILD)/go.img,$(QEMU_COMMON)) -m $(GO_MEM) $(DATA_DRIVE) -serial mon:stdio -display $(GO_DISPLAY)

# busybox, interactive (W10-S08, vi since W13-S01): needs `make busybox`
# first. At burrow> type `run /bin/busybox sh`; vi draws on the screen
# console (VT100 since W13-S02) and on this terminal (the serial line).
run-busybox: $(BUILD)/go-busybox.img $(BUILD)/data.img
	$(QEMU) $(subst $(BUILD)/disk.img,$(BUILD)/go-busybox.img,$(QEMU_COMMON)) -m $(GO_MEM) $(DATA_DRIVE) -serial mon:stdio -display $(GO_DISPLAY)

# Doom, interactive (W11): needs `make doom` first. At burrow> type
#   run /bin/doom -iwad /mnt/freedoom1.wad
run-doom: $(BUILD)/go-doom.img $(BUILD)/doom-data.img
	$(QEMU) $(subst $(BUILD)/disk.img,$(BUILD)/go-doom.img,$(QEMU_COMMON)) -m $(GO_MEM) $(DOOM_DRIVE) -serial mon:stdio -display $(GO_DISPLAY)

ktest: $(patsubst %,$(BUILD)/ktest-%.img,$(KTEST_SUITES))
	@for s in $(KTEST_SUITES); do QEMU="$(QEMU)" tests/smoke/smoke.sh $(BUILD)/ktest-$$s.img tests/ktest/expect/$$s.txt || exit 1; done

regress: $(BUILD)/disk.img $(BUILD)/go.img
	QEMU="$(QEMU)" tests/regression/run_all.sh $(BUILD)/disk.img

# PIT-09: the Go runtime's syscall set (host strace) vs the pinned census.
census:
	GO="$(GO)" tests/regression/census.sh $(BUILD)

# kernel/fs/fat against mkfs.fat/fsck.fat/mtools, both ways (skips without them).
fat-interop: $(BUILD)/data.img
	GO="$(GO)" tests/regression/fat_interop.sh $(BUILD)

sizes: $(BUILD)/stage1.bin $(BUILD)/stage2.bin $(BUILD)/loader.bin $(BUILD)/nucleus.disk.elf
	@scripts/sizes.sh $(BUILD)

$(BUILD) $(BUILD)/loader $(BUILD)/nucleus $(BUILD)/user:
	mkdir -p $@

docs-check:
	cd tools/doccheck && npm install --silent && node check.mjs $(addprefix ../../,$(shell find . -name '*.md' -not -path '*/node_modules/*' -not -path './build/*'))

# The documentation website (docs/06-guides/PUBLISHING_THE_SITE.md): every
# Markdown file, the plan's progress and the source tree, in build/site.
site:
	cd tools/site && npm install --silent && node build.mjs ../../$(BUILD)/site && node check-links.mjs ../../$(BUILD)/site

clean:
	rm -rf $(BUILD)
